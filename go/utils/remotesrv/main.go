package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/liquidata-inc/ld/dolt/go/gen/proto/dolt/services/remotesapi_v1alpha1"
	"github.com/liquidata-inc/ld/dolt/go/libraries/utils/filesys"
	"google.golang.org/grpc"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
)

func main() {
	dirParam := flag.String("dir", "", "root directory that this command will run in.")
	grpcPortParam := flag.Int("grpc-port", -1, "root directory that this command will run in.")
	httpPortParam := flag.Int("http-port", -1, "root directory that this command will run in.")
	flag.Parse()

	if dirParam != nil && len(*dirParam) > 0 {
		err := os.Chdir(*dirParam)

		if err != nil {
			log.Fatalln("failed to chdir to:", *dirParam)
			log.Fatalln("error:", err.Error())
			os.Exit(1)
		} else {
			log.Println("cwd set to " + *dirParam)
		}
	} else {
		log.Println("'dir' parameter not provided. Using the current working dir.")
	}

	httpHost := "localhost"

	if *httpPortParam != -1 {
		httpHost = fmt.Sprintf("%s:%d", httpHost, *httpPortParam)
	} else {
		*httpPortParam = 80
		log.Println("'http-port' parameter not provided. Using default port 80")
	}

	if *grpcPortParam == -1 {
		*grpcPortParam = 50051
		log.Println("'grpc-port' parameter not provided. Using default port 50051")
	}

	stopChan, wg := startServer(httpHost, *httpPortParam, *grpcPortParam)

	close(stopChan)
	wg.Wait()
}

func startServer(httpHost string, httpPort, grpcPort int) (chan interface{}, *sync.WaitGroup) {
	wg := sync.WaitGroup{}
	stopChan := make(chan interface{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		httpServer(httpPort, stopChan)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		grpcServer(httpHost, grpcPort, stopChan)
	}()

	oneByte := [1]byte{}
	for {
		_, err := os.Stdin.Read(oneByte[:])

		if err != nil || oneByte[0] == '\n' {
			break
		}
	}

	return stopChan, &wg
}

func grpcServer(httpHost string, grpcPort int, stopChan chan interface{}) {
	defer func() {
		log.Println("exiting grpc Server go routine")
	}()

	dbCache := NewLocalCSCache(filesys.LocalFS)
	chnkSt := NewHttpFSBackedChunkStore(httpHost, dbCache)

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", grpcPort))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer(grpc.MaxRecvMsgSize(128 * 1024 * 1024))
	go func() {
		remotesapi.RegisterChunkStoreServiceServer(grpcServer, chnkSt)

		log.Println("Starting grpc server on port", grpcPort)
		err := grpcServer.Serve(lis)
		log.Println("grpc server exited. error:", err)
	}()

	<-stopChan
	grpcServer.GracefulStop()
}

func httpServer(httpPort int, stopChan chan interface{}) {
	defer func() {
		log.Println("exiting http Server go routine")
	}()

	server := http.Server{
		Addr:    fmt.Sprintf(":%d", httpPort),
		Handler: http.HandlerFunc(ServeHTTP),
	}

	go func() {
		log.Println("Starting http server on port ", httpPort)
		err := server.ListenAndServe()
		log.Println("http server exited. exit error:", err)
	}()

	<-stopChan
	server.Shutdown(context.Background())
}

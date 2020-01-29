#!/usr/bin/env bats
load $BATS_TEST_DIRNAME/helper/common.bash

setup() {
    setup_common
}

teardown() {
    teardown_common
}

@test "ls empty creds" {
    run dolt creds ls
    [ "$status" -eq 0 ]
    [ "${#lines[@]}" -eq 0 ]
}

@test "ls new cred" {
    dolt creds new
    run dolt creds ls
    [ "$status" -eq 0 ]
    [ "${#lines[@]}" -eq 1 ]
    [[ "${lines[0]}" =~ (^\*\ ) ]] || false
    cred=`echo "${lines[0]}" | awk '{print $2}'`
    dolt creds new
    run dolt creds ls
    [ "$status" -eq 0 ]
    [ "${#lines[@]}" -eq 2 ]
    # Initially chosen credentials is still the chosen one.
    [[ "`echo "$output"|grep "$cred"`" =~ (^\*\ ) ]] || false
}

@test "ls -v new creds" {
    dolt creds new
    dolt creds new
    run dolt creds ls -v
    [ "$status" -eq 0 ]
    [ "${#lines[@]}" -eq 4 ]
    [[ "${lines[0]}" =~ (public\ key) ]] || false
    [[ "${lines[0]}" =~ (key\ id) ]] || false
}

@test "rm removes a cred" {
    dolt creds new
    run dolt creds ls
    [ "$status" -eq 0 ]
    [ "${#lines[@]}" -eq 1 ]
    cred=`echo "${lines[0]}" | awk '{print $2}'`
    dolt creds rm $cred
    run dolt creds ls
    [ "$status" -eq 0 ]
    [ "${#lines[@]}" -eq 0 ]
}
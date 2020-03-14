// Copyright 2020 Liquidata, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package testcommands

import (
	"context"
	"github.com/liquidata-inc/dolt/go/cmd/dolt/errhand"
	"testing"
	"time"

	sqle "github.com/src-d/go-mysql-server"
	"github.com/src-d/go-mysql-server/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/liquidata-inc/dolt/go/libraries/doltcore/doltdb"
	"github.com/liquidata-inc/dolt/go/libraries/doltcore/env"
	"github.com/liquidata-inc/dolt/go/libraries/doltcore/env/actions"
	"github.com/liquidata-inc/dolt/go/libraries/doltcore/merge"
	dsqle "github.com/liquidata-inc/dolt/go/libraries/doltcore/sqle"
)

type Command interface {
	CommandName() string
	Exec(t *testing.T, dEnv *env.DoltEnv) error
}

type StageAll struct{}

func (a StageAll) CommandName() string { return "stage_all" }

func (a StageAll) Exec(t *testing.T, dEnv *env.DoltEnv) error {
	return actions.StageAllTables(context.Background(), dEnv, false)
}

// TODO: comments on exported functions
type CommitStaged struct {
	Message string
}

func (c CommitStaged) CommandName() string { return "commit_staged" }

func (c CommitStaged) Exec(t *testing.T, dEnv *env.DoltEnv) error {
	return actions.CommitStaged(context.Background(), dEnv, c.Message, time.Now(), false)
}

type CommitAll struct {
	Message string
}

// CommandName returns "commit".
func (c CommitAll) CommandName() string { return "commit" }

// Exec executes a CommitAll command on a test dolt environment.
func (c CommitAll) Exec(t *testing.T, dEnv *env.DoltEnv) error {
	err := actions.StageAllTables(context.Background(), dEnv, false)
	require.NoError(t, err)

	return actions.CommitStaged(context.Background(), dEnv, c.Message, time.Now(), false)
}

// TODO: comments on exported functions
type ResetHard struct{}

func (r ResetHard) CommandName() string { return "reset_hard" }

// NOTE: does not handle untracked tables
func (r ResetHard) Exec(t *testing.T, dEnv *env.DoltEnv) error {
	headRoot, err := dEnv.HeadRoot(context.Background())
	if err != nil {
		return err
	}

	err = dEnv.UpdateWorkingRoot(context.Background(), headRoot)
	if err != nil {
		return err
	}

	_, err = dEnv.UpdateStagedRoot(context.Background(), headRoot)
	if err != nil {
		return err
	}

	err = actions.SaveTrackedDocsFromWorking(context.Background(), dEnv)
	return err
}

type Query struct {
	Query string
}

// CommandName returns "query".
func (q Query) CommandName() string { return "query" }

// Exec executes a Query command on a test dolt environment.
func (q Query) Exec(t *testing.T, dEnv *env.DoltEnv) error {
	root, err := dEnv.WorkingRoot(context.Background())
	require.NoError(t, err)
	sqlDb := dsqle.NewDatabase("dolt", root, nil, nil)
	engine := sqle.NewDefault()
	engine.AddDatabase(sqlDb)
	err = engine.Init()
	require.NoError(t, err)
	sqlCtx := sql.NewContext(context.Background())
	_, _, err = engine.Query(sqlCtx, q.Query)

	if err != nil {
		return err
	}

	err = dEnv.UpdateWorkingRoot(context.Background(), sqlDb.Root())
	return err
}

type Branch struct {
	BranchName string
}

// CommandName returns "branch".
func (b Branch) CommandName() string { return "branch" }

// Exec executes a Branch command on a test dolt environment.
func (b Branch) Exec(_ *testing.T, dEnv *env.DoltEnv) error {
	cwb := dEnv.RepoState.Head.Ref.String()
	return actions.CreateBranch(context.Background(), dEnv, b.BranchName, cwb, false)
}

type Checkout struct {
	BranchName string
}

// CommandName returns "checkout".
func (c Checkout) CommandName() string { return "checkout" }

// Exec executes a Checkout command on a test dolt environment.
func (c Checkout) Exec(_ *testing.T, dEnv *env.DoltEnv) error {
	return actions.CheckoutBranch(context.Background(), dEnv, c.BranchName)
}

type Merge struct {
	BranchName string
}

// CommandName returns "merge".
func (m Merge) CommandName() string { return "merge" }

// Exec executes a Merge command on a test dolt environment.
func (m Merge) Exec(t *testing.T, dEnv *env.DoltEnv) error {
	// Adapted from commands/merge.go:Exec()
	dref, err := dEnv.FindRef(context.Background(), m.BranchName)
	assert.NoError(t, err)

	cm1 := resolveCommit(t, "HEAD", dEnv)
	cm2 := resolveCommit(t, dref.String(), dEnv)

	h1, err := cm1.HashOf()
	assert.NoError(t, err)

	h2, err := cm2.HashOf()
	assert.NoError(t, err)
	assert.NotEqual(t, h1, h2)

	tblNames, err := dEnv.MergeWouldStompChanges(context.Background(), cm2)
	if err != nil {
		return err
	}
	if len(tblNames) != 0 {
		return errhand.BuildDError("error: failed to determine mergability.").AddCause(err).Build()
	}

	if ok, err := cm1.CanFastForwardTo(context.Background(), cm2); ok {
		if err != nil {
			return err
		}

		rv, err := cm2.GetRootValue()
		assert.NoError(t, err)

		h, err := dEnv.DoltDB.WriteRootValue(context.Background(), rv)
		assert.NoError(t, err)

		err = dEnv.DoltDB.FastForward(context.Background(), dEnv.RepoState.CWBHeadRef(), cm2)
		if err != nil {
			return err
		}

		dEnv.RepoState.Working = h.String()
		dEnv.RepoState.Staged = h.String()
		err = dEnv.RepoState.Save(dEnv.FS)
		assert.NoError(t, err)

		err = actions.SaveTrackedDocsFromWorking(context.Background(), dEnv)
		assert.NoError(t, err)

	} else {
		mergedRoot, tblToStats, err := merge.MergeCommits(context.Background(), dEnv.DoltDB, cm1, cm2)
		for _, stats := range tblToStats {
			assert.True(t, stats.Conflicts == 0)
		}

		h2, err := cm2.HashOf()
		assert.NoError(t, err)

		err = dEnv.RepoState.StartMerge(dref, h2.String(), dEnv.FS)
		if err != nil {
			return err
		}

		err = dEnv.UpdateWorkingRoot(context.Background(), mergedRoot)
		if err != nil {
			return err
		}

		err = actions.SaveTrackedDocsFromWorking(context.Background(), dEnv)
		if err != nil {
			return err
		}

		_, err = dEnv.UpdateStagedRoot(context.Background(), mergedRoot)
		if err != nil {
			return err
		}
	}
	return nil
}

func resolveCommit(t *testing.T, cSpecStr string, dEnv *env.DoltEnv) *doltdb.Commit {
	cs, err := doltdb.NewCommitSpec(cSpecStr, dEnv.RepoState.Head.Ref.String())
	require.NoError(t, err)
	cm, err := dEnv.DoltDB.Resolve(context.TODO(), cs)
	require.NoError(t, err)
	return cm
}
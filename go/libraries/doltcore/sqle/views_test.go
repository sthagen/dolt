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

package sqle

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/liquidata-inc/dolt/go/libraries/doltcore/dtestutils"
	"github.com/liquidata-inc/dolt/go/libraries/doltcore/row"
	. "github.com/liquidata-inc/dolt/go/libraries/doltcore/sql/sqltestutil"
	"github.com/liquidata-inc/dolt/go/store/types"
)

// Not an exhaustive test of views -- we rely on bats tests for end-to-end verification.
func TestViews(t *testing.T) {
	dEnv := dtestutils.CreateTestEnv()

	ctx := context.Background()
	root, _ := dEnv.WorkingRoot(ctx)

	var err error
	root, err = ExecuteSql(dEnv, root, "create table test (a int primary key)")
	require.NoError(t, err)

	root, err = ExecuteSql(dEnv, root, "insert into test values (1), (2), (3)")
	require.NoError(t, err)

	root, err = ExecuteSql(dEnv, root, "create view plus1 as select a + 1 from test")
	require.NoError(t, err)

	expectedSchema := NewResultSetSchema("a", types.IntKind)
	expectedRows := []row.Row{
		NewResultSetRow(types.Int(2)),
		NewResultSetRow(types.Int(3)),
		NewResultSetRow(types.Int(4)),
	}
	rows, _, err := executeSelect(context.Background(), dEnv, expectedSchema, root, "select * from plus1")
	require.NoError(t, err)
	assert.Equal(t, expectedRows, rows)

	root, err = ExecuteSql(dEnv, root, "drop view plus1")
	require.NoError(t, err)
}

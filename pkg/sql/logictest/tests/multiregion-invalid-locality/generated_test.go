// Copyright 2022 The Cockroach Authors.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

// Code generated by generate-logictest, DO NOT EDIT.

package testmultiregion_invalid_locality

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cockroachdb/cockroach/pkg/build/bazel"
	"github.com/cockroachdb/cockroach/pkg/security/securityassets"
	"github.com/cockroachdb/cockroach/pkg/security/securitytest"
	"github.com/cockroachdb/cockroach/pkg/server"
	"github.com/cockroachdb/cockroach/pkg/sql/logictest"
	"github.com/cockroachdb/cockroach/pkg/testutils/serverutils"
	"github.com/cockroachdb/cockroach/pkg/testutils/skip"
	"github.com/cockroachdb/cockroach/pkg/testutils/testcluster"
	"github.com/cockroachdb/cockroach/pkg/util/leaktest"
	"github.com/cockroachdb/cockroach/pkg/util"
	"github.com/cockroachdb/cockroach/pkg/util/randutil"
)

const configIdx = 10

var logicTestDir string

func init() {
	if bazel.BuiltWithBazel() {
		var err error
		logicTestDir, err = bazel.Runfile("pkg/sql/logictest/testdata/logic_test")
		if err != nil {
			panic(err)
		}
	} else {
		logicTestDir = "../../../../sql/logictest/testdata/logic_test"
	}
}

func TestMain(m *testing.M) {
	securityassets.SetLoader(securitytest.EmbeddedAssets)
	util.LogMetamorphicVariables()
	randutil.SeedForTests()
	serverutils.InitTestServerFactory(server.TestServerFactory)
	serverutils.InitTestClusterFactory(testcluster.TestClusterFactory)
	os.Exit(m.Run())
}

func runLogicTest(t *testing.T, file string) {
	skip.UnderDeadlock(t, "times out and/or hangs")
	logictest.RunLogicTest(t, logictest.TestServerArgs{}, configIdx, filepath.Join(logicTestDir, file))
}

// TestLogic_tmp runs any tests that are prefixed with "_", in which a dedicated
// test is not generated for. This allows developers to create and run temporary
// test files that are not checked into the repository, without repeatedly
// regenerating and reverting changes to this file, generated_test.go.
//
// TODO(mgartner): Add file filtering so that individual files can be run,
// instead of all files with the "_" prefix.
func TestLogic_tmp(t *testing.T) {
	defer leaktest.AfterTest(t)()
	var glob string
	glob = filepath.Join(logicTestDir, "_*")
	logictest.RunLogicTests(t, logictest.TestServerArgs{}, configIdx, glob)
}

func TestLogic_multiregion_invalid_locality(
	t *testing.T,
) {
	defer leaktest.AfterTest(t)()
	runLogicTest(t, "multiregion_invalid_locality")
}

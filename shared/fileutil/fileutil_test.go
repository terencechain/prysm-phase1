// Copyright 2015 The go-ethereum Authors
// This file is part of go-ethereum.
//
// go-ethereum is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// go-ethereum is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with go-ethereum. If not, see <http://www.gnu.org/licenses/>.
package fileutil

import (
	"os"
	"os/user"
	"testing"

	"github.com/prysmaticlabs/prysm/shared/testutil/assert"
	"github.com/prysmaticlabs/prysm/shared/testutil/require"
)

func TestPathExpansion(t *testing.T) {
	user, err := user.Current()
	require.NoError(t, err)
	tests := map[string]string{
		"/home/someuser/tmp": "/home/someuser/tmp",
		"~/tmp":              user.HomeDir + "/tmp",
		"$DDDXXX/a/b":        "/tmp/a/b",
		"/a/b/":              "/a/b",
	}
	require.NoError(t, os.Setenv("DDDXXX", "/tmp"))
	for test, expected := range tests {
		expanded, err := ExpandPath(test)
		require.NoError(t, err)
		assert.Equal(t, expected, expanded)
	}
}

// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package task

import (
	"testing"

	"github.com/shoenig/test/must"
)

func TestStore_crud_operations(t *testing.T) {
	s := NewStore()

	id := "aaaaa"

	// Get an id that is not set
	result, exists := s.Get(id)
	must.False(t, exists)
	must.Nil(t, result)

	// Set a handle for id
	handle := &Handle{
		pid: 1000,
	}
	s.Set(id, handle)

	// Get our handle for id
	result, exists = s.Get(id)
	must.True(t, exists)
	must.Eq(t, handle, result)

	// Delete our handle for id
	s.Del(id)

	// The handle should be gone
	result, exists = s.Get(id)
	must.False(t, exists)
	must.Nil(t, result)
}

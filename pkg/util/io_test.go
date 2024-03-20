// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package util

import (
	"bytes"
	"io"
	"testing"

	"github.com/shoenig/test/must"
)

func Test_NullCloser(t *testing.T) {
	b := new(bytes.Buffer)
	wc := NullCloser(b)
	_, err := io.WriteString(wc, "hello")
	must.NoError(t, err)
	must.NoError(t, wc.Close())
	must.Eq(t, "hello", b.String())
}

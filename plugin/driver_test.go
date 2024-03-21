// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package plugin

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/hashicorp/nomad/client/testutil"
	"github.com/hashicorp/nomad/plugins/drivers"
	"github.com/hashicorp/nomad/plugins/shared/structs"
	"github.com/shoenig/test/must"
)

func Test_doFingerprint_normal(t *testing.T) {
	testutil.RequireRoot(t)

	p := new(Plugin)
	p.config = &Config{
		UnveilByTask:   true,
		UnveilDefaults: true,
	}
	fp := p.doFingerprint(exec.LookPath)

	must.Eq(t, drivers.HealthStateHealthy, fp.Health)
	must.Eq(t, drivers.DriverHealthy, fp.HealthDescription)
	must.Eq(t, map[string]*structs.Attribute{
		"driver.exec2.unveil.tasks":    structs.NewBoolAttribute(true),
		"driver.exec2.unveil.defaults": structs.NewBoolAttribute(true),
	}, fp.Attributes)
}

func Test_doFingerprint_notRoot(t *testing.T) {
	testutil.RequireNonRoot(t)

	p := new(Plugin)
	fp := p.doFingerprint(nil)

	must.Eq(t, drivers.HealthStateUndetected, fp.Health)
	must.Eq(t, drivers.DriverRequiresRootMessage, fp.HealthDescription)
}

func Test_doFingerprint_missing_nsenter(t *testing.T) {
	testutil.RequireRoot(t)

	nsenterLookupFailure := func(name string) (string, error) {
		if name == "nsenter" {
			return "", os.ErrNotExist
		}
		return filepath.Join("/bin", name), nil
	}

	p := new(Plugin)
	fp := p.doFingerprint(nsenterLookupFailure)

	must.Eq(t, drivers.HealthStateUndetected, fp.Health)
	must.Eq(t, "nsenter executable not found", fp.HealthDescription)
}

func Test_doFingerprint_missing_unshare(t *testing.T) {
	testutil.RequireRoot(t)

	unshareLookupFailure := func(name string) (string, error) {
		if name == "unshare" {
			return "", os.ErrNotExist
		}
		return filepath.Join("/bin", name), nil
	}

	p := new(Plugin)
	fp := p.doFingerprint(unshareLookupFailure)

	must.Eq(t, drivers.HealthStateUndetected, fp.Health)
	must.Eq(t, "unshare executable not found", fp.HealthDescription)
}

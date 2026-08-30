package main

import (
	"testing"

	"wrnrs/internal/app"
	"wrnrs/internal/objectstore"
)

// TestObjectStoreHelpersReturnGenuinelyNilInterfaceWhenStoreIsNil is the
// regression test for the typed-nil panic: with MINIO_ACCESS_KEY/
// MINIO_SECRET_KEY unset, minioStore and positionsStore in run() stay a nil
// *objectstore.MinIOStore. Assigning that nil pointer straight into an
// interface-typed field (app.Options.ObjectStore, positions.HandlerOptions.
// ObjectStore) boxes a typed nil into a NON-nil interface — every `!= nil`
// guard downstream would then incorrectly report the store as configured,
// and the first call (e.g. from the positions bulk-dump goroutine) would
// panic on a nil receiver, with no recover() anywhere in this repository to
// catch it. appObjectStore/positionsObjectStore exist specifically to keep
// that from happening; this test proves they do.
func TestObjectStoreHelpersReturnGenuinelyNilInterfaceWhenStoreIsNil(t *testing.T) {
	var unconfigured *objectstore.MinIOStore // nil: what run() leaves minioStore/positionsStore as when MinIO is unconfigured

	if got := appObjectStore(unconfigured); got != nil {
		t.Fatalf("appObjectStore(nil) = %#v, want a genuinely nil app.ObjectStore interface value", got)
	}
	if got := positionsObjectStore(unconfigured); got != nil {
		t.Fatalf("positionsObjectStore(nil) = %#v, want a genuinely nil positions.ObjectStore interface value", got)
	}

	// Sanity check that this is a real footgun and not a strawman: assigning
	// the nil concrete pointer directly to an interface variable, bypassing
	// the guard, produces a NON-nil interface. If this ever started
	// reporting nil, Go's interface semantics would have changed and the
	// rest of this test would no longer be testing anything meaningful.
	var naive app.ObjectStore = unconfigured
	if naive == nil {
		t.Fatal("test invariant broken: a *objectstore.MinIOStore(nil) assigned directly to an app.ObjectStore interface variable was reported nil; the typed-nil hazard this test guards against no longer reproduces the way this test assumes")
	}
}

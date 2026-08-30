package main

import (
	"testing"

	"wrnrs/internal/app"
	"wrnrs/internal/config"
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

// newTestMinIOStore builds a real, non-nil *objectstore.MinIOStore for
// tests. objectstore.NewMinIOStore only constructs a minio SDK client
// in-process (minio.New does no network I/O), so this is safe to call
// without a MinIO server present.
func newTestMinIOStore(t *testing.T) *objectstore.MinIOStore {
	t.Helper()
	store, err := objectstore.NewMinIOStore(objectstore.MinIOConfig{
		Endpoint:  "localhost:9000",
		AccessKey: "test-access-key",
		SecretKey: "test-secret-key",
		Bucket:    "test-bucket",
	})
	if err != nil {
		t.Fatalf("objectstore.NewMinIOStore: %v", err)
	}
	return store
}

// TestBuildAppOptionsRoutesObjectStoreThroughHelper pins the ObjectStore
// wiring at its actual call site in run(): buildAppOptions is the exact
// function run() calls to construct app.Options, not a proxy for it. Unlike
// TestObjectStoreHelpersReturnGenuinelyNilInterfaceWhenStoreIsNil above
// (which only proves appObjectStore itself behaves correctly in isolation),
// this test fails if buildAppOptions's ObjectStore field is ever changed to
// a direct `minioStore` assignment that bypasses appObjectStore — the exact
// mutation that reintroduces the original typed-nil panic.
func TestBuildAppOptionsRoutesObjectStoreThroughHelper(t *testing.T) {
	t.Run("nil store yields a genuinely nil interface", func(t *testing.T) {
		var unconfigured *objectstore.MinIOStore
		options := buildAppOptions(config.Config{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, unconfigured, nil)
		if options.ObjectStore != nil {
			t.Fatalf("buildAppOptions(...).ObjectStore = %#v, want nil", options.ObjectStore)
		}
	})

	t.Run("non-nil store is populated", func(t *testing.T) {
		store := newTestMinIOStore(t)
		options := buildAppOptions(config.Config{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, store, nil)
		if options.ObjectStore == nil {
			t.Fatal("buildAppOptions(...).ObjectStore = nil, want a populated app.ObjectStore")
		}
	})
}

// TestBuildPositionsHandlerOptionsRoutesObjectStoreThroughHelper is the
// positions.HandlerOptions analogue of
// TestBuildAppOptionsRoutesObjectStoreThroughHelper above: it calls
// buildPositionsHandlerOptions, the exact function run() uses to construct
// positions.HandlerOptions, so a future edit reverting its ObjectStore field
// to a direct `positionsStore` assignment breaks this test.
func TestBuildPositionsHandlerOptionsRoutesObjectStoreThroughHelper(t *testing.T) {
	t.Run("nil store yields a genuinely nil interface", func(t *testing.T) {
		var unconfigured *objectstore.MinIOStore
		options := buildPositionsHandlerOptions(nil, nil, nil, nil, nil, unconfigured, nil)
		if options.ObjectStore != nil {
			t.Fatalf("buildPositionsHandlerOptions(...).ObjectStore = %#v, want nil", options.ObjectStore)
		}
	})

	t.Run("non-nil store is populated", func(t *testing.T) {
		store := newTestMinIOStore(t)
		options := buildPositionsHandlerOptions(nil, nil, nil, nil, nil, store, nil)
		if options.ObjectStore == nil {
			t.Fatal("buildPositionsHandlerOptions(...).ObjectStore = nil, want a populated positions.ObjectStore")
		}
	})
}

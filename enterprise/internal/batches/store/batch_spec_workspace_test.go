package store

import (
	"context"
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"

	ct "github.com/sourcegraph/sourcegraph/enterprise/internal/batches/testing"
	btypes "github.com/sourcegraph/sourcegraph/enterprise/internal/batches/types"
	"github.com/sourcegraph/sourcegraph/internal/database"
	"github.com/sourcegraph/sourcegraph/internal/extsvc"
	"github.com/sourcegraph/sourcegraph/internal/types"
	batcheslib "github.com/sourcegraph/sourcegraph/lib/batches"
)

func testStoreBatchSpecWorkspaces(t *testing.T, ctx context.Context, s *Store, clock ct.Clock) {
	repoStore := database.ReposWith(s)
	esStore := database.ExternalServicesWith(s)

	repo := ct.TestRepo(t, esStore, extsvc.KindGitHub)
	deletedRepo := ct.TestRepo(t, esStore, extsvc.KindGitHub).With(types.Opt.RepoDeletedAt(clock.Now()))

	if err := repoStore.Create(ctx, repo, deletedRepo); err != nil {
		t.Fatal(err)
	}
	if err := repoStore.Delete(ctx, deletedRepo.ID); err != nil {
		t.Fatal(err)
	}

	workspaces := make([]*btypes.BatchSpecWorkspace, 0, 3)
	for i := 0; i < cap(workspaces); i++ {
		job := &btypes.BatchSpecWorkspace{
			BatchSpecID:      int64(i + 567),
			ChangesetSpecIDs: []int64{int64(i + 456), int64(i + 678)},
			RepoID:           repo.ID,
			Branch:           "master",
			Commit:           "d34db33f",
			Path:             "sub/dir/ectory",
			FileMatches: []string{
				"a.go",
				"a/b/horse.go",
				"a/b/c.go",
			},
			Steps: []batcheslib.Step{
				{
					Run:       "complex command that changes code",
					Container: "alpine:3",
					Files: map[string]string{
						"/tmp/foobar.go": "package main",
					},
					Outputs: map[string]batcheslib.Output{
						"myOutput": {Value: `${{ step.stdout }}`},
					},
					If: `${{ eq repository.name "github.com/sourcegraph/sourcegraph" }}`,
				},
			},
			OnlyFetchWorkspace: true,
			Unsupported:        true,
			Ignored:            true,
			Skipped:            true,
		}

		if i == cap(workspaces)-1 {
			job.RepoID = deletedRepo.ID
		}

		workspaces = append(workspaces, job)
	}

	t.Run("Create", func(t *testing.T) {
		for _, job := range workspaces {
			if err := s.CreateBatchSpecWorkspace(ctx, job); err != nil {
				t.Fatal(err)
			}

			have := job
			if have.ID == 0 {
				t.Fatal("ID should not be zero")
			}

			want := have
			want.CreatedAt = clock.Now()
			want.UpdatedAt = clock.Now()

			if diff := cmp.Diff(have, want); diff != "" {
				t.Fatal(diff)
			}
		}
	})

	t.Run("Get", func(t *testing.T) {
		t.Run("GetByID", func(t *testing.T) {
			for i, job := range workspaces {
				t.Run(strconv.Itoa(i), func(t *testing.T) {
					have, err := s.GetBatchSpecWorkspace(ctx, GetBatchSpecWorkspaceOpts{ID: job.ID})

					if job.RepoID == deletedRepo.ID {
						if err != ErrNoResults {
							t.Fatalf("wrong error: %s", err)
						}
						return
					}

					if err != nil {
						t.Fatal(err)
					}

					if diff := cmp.Diff(have, job); diff != "" {
						t.Fatal(diff)
					}
				})
			}
		})

		t.Run("NoResults", func(t *testing.T) {
			opts := GetBatchSpecWorkspaceOpts{ID: 0xdeadbeef}

			_, have := s.GetBatchSpecWorkspace(ctx, opts)
			want := ErrNoResults

			if have != want {
				t.Fatalf("have err %v, want %v", have, want)
			}
		})
	})

	t.Run("List", func(t *testing.T) {
		t.Run("All", func(t *testing.T) {
			have, _, err := s.ListBatchSpecWorkspaces(ctx, ListBatchSpecWorkspacesOpts{})
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(have, workspaces[:len(workspaces)-1]); diff != "" {
				t.Fatalf("invalid jobs returned: %s", diff)
			}
		})

		t.Run("ByBatchSpecID", func(t *testing.T) {
			for _, ws := range workspaces {
				have, _, err := s.ListBatchSpecWorkspaces(ctx, ListBatchSpecWorkspacesOpts{
					BatchSpecID: ws.BatchSpecID,
				})

				if err != nil {
					t.Fatal(err)
				}

				if ws.RepoID == deletedRepo.ID {
					if len(have) != 0 {
						t.Fatalf("expected zero results, but got: %d", len(have))
					}
					return
				}
				if len(have) != 1 {
					t.Fatalf("wrong number of results. have=%d", len(have))
				}

				if diff := cmp.Diff(have, []*btypes.BatchSpecWorkspace{ws}); diff != "" {
					t.Fatalf("invalid jobs returned: %s", diff)
				}
			}
		})
	})

	t.Run("MarkSkippedBatchSpecWorkspaces", func(t *testing.T) {
		tests := []struct {
			batchSpec   *btypes.BatchSpec
			workspace   *btypes.BatchSpecWorkspace
			wantSkipped bool
		}{
			{
				batchSpec:   &btypes.BatchSpec{AllowIgnored: false, AllowUnsupported: false},
				workspace:   &btypes.BatchSpecWorkspace{Ignored: true, Steps: []batcheslib.Step{{Run: "test"}}},
				wantSkipped: true,
			},
			{
				batchSpec:   &btypes.BatchSpec{AllowIgnored: true, AllowUnsupported: false},
				workspace:   &btypes.BatchSpecWorkspace{Ignored: true, Steps: []batcheslib.Step{{Run: "test"}}},
				wantSkipped: false,
			},
			{
				batchSpec:   &btypes.BatchSpec{AllowIgnored: false, AllowUnsupported: false},
				workspace:   &btypes.BatchSpecWorkspace{Unsupported: true, Steps: []batcheslib.Step{{Run: "test"}}},
				wantSkipped: true,
			},
			{
				batchSpec:   &btypes.BatchSpec{AllowIgnored: false, AllowUnsupported: true},
				workspace:   &btypes.BatchSpecWorkspace{Unsupported: true, Steps: []batcheslib.Step{{Run: "test"}}},
				wantSkipped: false,
			},
			{
				batchSpec:   &btypes.BatchSpec{AllowIgnored: true, AllowUnsupported: true},
				workspace:   &btypes.BatchSpecWorkspace{Steps: []batcheslib.Step{}},
				wantSkipped: true,
			},
		}

		for _, tt := range tests {
			tt.batchSpec.NamespaceUserID = 1
			tt.batchSpec.UserID = 1
			err := s.CreateBatchSpec(ctx, tt.batchSpec)
			if err != nil {
				t.Fatal(err)
			}

			tt.workspace.BatchSpecID = tt.batchSpec.ID
			tt.workspace.RepoID = repo.ID
			tt.workspace.Branch = "master"
			tt.workspace.Commit = "d34db33f"
			tt.workspace.Path = "sub/dir/ectory"
			tt.workspace.FileMatches = []string{}

			if err := s.CreateBatchSpecWorkspace(ctx, tt.workspace); err != nil {
				t.Fatal(err)
			}

			if err := s.MarkSkippedBatchSpecWorkspaces(ctx, tt.batchSpec.ID); err != nil {
				t.Fatal(err)
			}

			reloaded, err := s.GetBatchSpecWorkspace(ctx, GetBatchSpecWorkspaceOpts{ID: tt.workspace.ID})
			if err != nil {
				t.Fatal(err)
			}

			if want, have := tt.wantSkipped, reloaded.Skipped; have != want {
				t.Fatalf("workspace.Skipped is wrong. want=%t, have=%t", want, have)
			}
		}
	})
}

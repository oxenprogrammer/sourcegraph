package backend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/google/go-cmp/cmp"
	"github.com/google/zoekt"

	"github.com/sourcegraph/sourcegraph/schema"
)

func TestGetIndexOptions(t *testing.T) {
	withBranches := func(c schema.SiteConfiguration, b map[string][]string) schema.SiteConfiguration {
		if c.ExperimentalFeatures == nil {
			c.ExperimentalFeatures = &schema.ExperimentalFeatures{}
		}
		c.ExperimentalFeatures.SearchIndexBranches = b
		return c
	}

	type caseT struct {
		name              string
		conf              schema.SiteConfiguration
		searchContextRevs []string
		repo              string
		want              zoektIndexOptions
	}

	cases := []caseT{{
		name: "default",
		conf: schema.SiteConfiguration{},
		repo: "repo",
		want: zoektIndexOptions{
			RepoID:  1,
			Symbols: true,
			Branches: []zoekt.RepositoryBranch{
				{Name: "HEAD", Version: "!HEAD"},
			},
		},
	}, {
		name: "public",
		conf: schema.SiteConfiguration{},
		repo: "public",
		want: zoektIndexOptions{
			RepoID:  5,
			Public:  true,
			Symbols: true,
			Branches: []zoekt.RepositoryBranch{
				{Name: "HEAD", Version: "!HEAD"},
			},
		},
	}, {
		name: "fork",
		conf: schema.SiteConfiguration{},
		repo: "fork",
		want: zoektIndexOptions{
			RepoID:  6,
			Fork:    true,
			Symbols: true,
			Branches: []zoekt.RepositoryBranch{
				{Name: "HEAD", Version: "!HEAD"},
			},
		},
	}, {
		name: "archived",
		conf: schema.SiteConfiguration{},
		repo: "archived",
		want: zoektIndexOptions{
			RepoID:   7,
			Archived: true,
			Symbols:  true,
			Branches: []zoekt.RepositoryBranch{
				{Name: "HEAD", Version: "!HEAD"},
			},
		},
	}, {
		name: "nosymbols",
		conf: schema.SiteConfiguration{
			SearchIndexSymbolsEnabled: boolPtr(false)},
		repo: "repo",
		want: zoektIndexOptions{
			RepoID: 1,
			Branches: []zoekt.RepositoryBranch{
				{Name: "HEAD", Version: "!HEAD"},
			},
		},
	}, {
		name: "largefiles",
		conf: schema.SiteConfiguration{
			SearchLargeFiles: []string{"**/*.jar", "*.bin"},
		},
		repo: "repo",
		want: zoektIndexOptions{
			RepoID:     1,
			Symbols:    true,
			LargeFiles: []string{"**/*.jar", "*.bin"},
			Branches: []zoekt.RepositoryBranch{
				{Name: "HEAD", Version: "!HEAD"},
			},
		},
	}, {
		name: "conf index branches",
		conf: withBranches(schema.SiteConfiguration{}, map[string][]string{"repo": {"a"}}),
		repo: "repo",
		want: zoektIndexOptions{
			RepoID:  1,
			Symbols: true,
			Branches: []zoekt.RepositoryBranch{
				{Name: "HEAD", Version: "!HEAD"},
				{Name: "a", Version: "!a"},
			},
		},
	}, {
		name:              "with search context revisions",
		conf:              schema.SiteConfiguration{},
		repo:              "repo",
		searchContextRevs: []string{"rev1", "rev2"},
		want: zoektIndexOptions{
			RepoID:  1,
			Symbols: true,
			Branches: []zoekt.RepositoryBranch{
				{Name: "HEAD", Version: "!HEAD"},
				{Name: "rev1", Version: "!rev1"},
				{Name: "rev2", Version: "!rev2"},
			},
		},
	}, {
		name: "with a priority value",
		conf: schema.SiteConfiguration{},
		repo: "priority",
		want: zoektIndexOptions{
			RepoID:  4,
			Symbols: true,
			Branches: []zoekt.RepositoryBranch{
				{Name: "HEAD", Version: "!HEAD"},
			},
			Priority: 10,
		},
	}}

	{
		// Generate case for no more than than 64 branches
		var branches []string
		for i := 0; i < 100; i++ {
			branches = append(branches, fmt.Sprintf("%.2d", i))
		}
		want := []zoekt.RepositoryBranch{{Name: "HEAD", Version: "!HEAD"}}
		for i := 0; i < 63; i++ {
			want = append(want, zoekt.RepositoryBranch{
				Name:    fmt.Sprintf("%.2d", i),
				Version: fmt.Sprintf("!%.2d", i),
			})
		}
		cases = append(cases, caseT{
			name: "limit branches",
			conf: withBranches(schema.SiteConfiguration{}, map[string][]string{"repo": branches}),
			repo: "repo",
			want: zoektIndexOptions{
				RepoID:   1,
				Symbols:  true,
				Branches: want,
			},
		})
	}

	getRepoIndexOptions := func(repo string) (*RepoIndexOptions, error) {
		repoID := int32(1)
		for _, r := range []string{"repo", "foo", "not_in_version_context", "priority", "public", "fork", "archived"} {
			if r == repo {
				break
			}
			repoID++
		}
		var priority float64
		if repo == "priority" {
			priority = 10
		}
		return &RepoIndexOptions{
			RepoID:   repoID,
			Public:   repo == "public",
			Fork:     repo == "fork",
			Archived: repo == "archived",
			Priority: priority,
			GetVersion: func(branch string) (string, error) {
				return "!" + branch, nil
			},
		}, nil
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			getSearchContextRevisions := func(int32) ([]string, error) { return tc.searchContextRevs, nil }

			b := GetIndexOptions(&tc.conf, getRepoIndexOptions, getSearchContextRevisions, tc.repo)

			var got zoektIndexOptions
			if err := json.Unmarshal(b, &got); err != nil {
				t.Fatal(err)
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatal("mismatch (-want, +got):\n", diff)
			}
		})
	}
}

func TestGetIndexOptions_getVersion(t *testing.T) {
	conf := schema.SiteConfiguration{}
	getSearchContextRevs := func(int32) ([]string, error) { return []string{"b1", "b2"}, nil }

	boom := errors.New("boom")
	cases := []struct {
		name    string
		f       func(string) (string, error)
		want    []zoekt.RepositoryBranch
		wantErr string
	}{{
		name: "error",
		f: func(_ string) (string, error) {
			return "", boom
		},
		wantErr: "boom",
	}, {
		// no HEAD means we don't index anything. This leads to zoekt having
		// an empty index.
		name: "no HEAD",
		f: func(branch string) (string, error) {
			if branch == "HEAD" {
				return "", nil
			}
			return "!" + branch, nil
		},
		want: nil,
	}, {
		name: "no branch",
		f: func(branch string) (string, error) {
			if branch == "b1" {
				return "", nil
			}
			return "!" + branch, nil
		},
		want: []zoekt.RepositoryBranch{
			{Name: "HEAD", Version: "!HEAD"},
			{Name: "b2", Version: "!b2"},
		},
	}, {
		name: "all",
		f: func(branch string) (string, error) {
			return "!" + branch, nil
		},
		want: []zoekt.RepositoryBranch{
			{Name: "HEAD", Version: "!HEAD"},
			{Name: "b1", Version: "!b1"},
			{Name: "b2", Version: "!b2"},
		},
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			getRepoIndexOptions := func(repo string) (*RepoIndexOptions, error) {
				return &RepoIndexOptions{
					GetVersion: tc.f,
				}, nil
			}

			b := GetIndexOptions(&conf, getRepoIndexOptions, getSearchContextRevs, "repo")

			var got zoektIndexOptions
			if err := json.Unmarshal(b, &got); err != nil {
				t.Fatal(err)
			}

			if got.Error != tc.wantErr {
				t.Fatalf("expected error %v, got body %s and error %v", tc.wantErr, b, got.Error)
			}
			if tc.wantErr != "" {
				return
			}

			if diff := cmp.Diff(tc.want, got.Branches); diff != "" {
				t.Fatal("mismatch (-want, +got):\n", diff)
			}
		})
	}
}

func TestGetIndexOptions_batch(t *testing.T) {
	var (
		repos []string
		want  []zoektIndexOptions
	)
	for i := 0; i < 100; i++ {
		if i%20 == 0 {
			repos = append(repos, fmt.Sprintf("error-%02d", i))
			want = append(want, zoektIndexOptions{Error: "error"})
		} else {
			repo := fmt.Sprintf("repo-%02d", i)
			repos = append(repos, repo)
			want = append(want, zoektIndexOptions{
				Symbols: true,
				Branches: []zoekt.RepositoryBranch{
					{Name: "HEAD", Version: "!HEAD-" + repo},
				},
			})
		}
	}
	getRepoIndexOptions := func(repo string) (*RepoIndexOptions, error) {
		return &RepoIndexOptions{
			GetVersion: func(branch string) (string, error) {
				if strings.HasPrefix(repo, "error") {
					return "", errors.New("error")
				}
				return fmt.Sprintf("!%s-%s", branch, repo), nil
			},
		}, nil
	}

	getSearchContextRevs := func(int32) ([]string, error) { return nil, nil }

	b := GetIndexOptions(&schema.SiteConfiguration{}, getRepoIndexOptions, getSearchContextRevs, repos...)
	dec := json.NewDecoder(bytes.NewReader(b))
	got := make([]zoektIndexOptions, len(repos))
	for i := range repos {
		if err := dec.Decode(&got[i]); err != nil {
			t.Fatal(err)
		}
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatal("mismatch (-want, +got):\n", diff)
	}
}

func boolPtr(b bool) *bool {
	return &b
}

package authz_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

	"github.com/Mmd4LIFE/pivot/internal/authz"
	"github.com/Mmd4LIFE/pivot/internal/store"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
)

// The declarative permission harness (P0-AUTHZ-006).
//
// The assertions live in testdata as data, not as Go. That matters for two
// reasons. A permission model is reviewed by people who should not have to read
// test code to check it — "may an analyst run raw SQL?" should be answerable by
// looking at one line. And a table of assertions can be run against a different
// implementation unchanged, which is what makes swapping the backing checker a
// verifiable change rather than a hopeful one.

// modelFile is the specification under test.
const modelFile = "testdata/model_v1.yaml"

// spec is the file's shape.
type spec struct {
	Fixture struct {
		Organization string   `yaml:"organization"`
		Users        []string `yaml:"users"`
		Groups       []struct {
			Name   string `yaml:"name"`
			Parent string `yaml:"parent"`
		} `yaml:"groups"`
		Members map[string][]string `yaml:"members"`
		Grants  []struct {
			Subject  string `yaml:"subject"`
			Relation string `yaml:"relation"`
			Object   string `yaml:"object"`
		} `yaml:"grants"`
	} `yaml:"fixture"`

	Assertions []struct {
		Subject    string `yaml:"subject"`
		Permission string `yaml:"permission"`
		Object     string `yaml:"object"`
		Expect     string `yaml:"expect"`
	} `yaml:"assertions"`
}

func loadSpec(t *testing.T) spec {
	t.Helper()

	data, err := os.ReadFile(filepath.Clean(modelFile))
	if err != nil {
		t.Fatalf("read %s: %v", modelFile, err)
	}

	var s spec
	if err := yaml.Unmarshal(data, &s); err != nil {
		t.Fatalf("parse %s: %v", modelFile, err)
	}

	if len(s.Assertions) == 0 {
		t.Fatalf("%s contains no assertions", modelFile)
	}

	return s
}

// world is a built fixture: symbolic names mapped onto real identifiers.
type world struct {
	repos *repo.Repositories
	ctx   context.Context

	orgID  uuid.UUID
	users  map[string]uuid.UUID
	groups map[string]uuid.UUID
}

// resolve turns a symbolic identifier from the file into a real one.
func (w *world) resolve(symbolic string) (string, error) {
	if id, ok := w.users[symbolic]; ok {
		return id.String(), nil
	}

	if id, ok := w.groups[symbolic]; ok {
		return id.String(), nil
	}

	if symbolic == "acme" {
		return w.orgID.String(), nil
	}

	return "", fmt.Errorf("unknown name %q in %s", symbolic, modelFile)
}

// resolveSubject parses and resolves a subject from the file.
func (w *world) resolveSubject(s string) (authz.Subject, error) {
	subject, err := authz.ParseSubject(s)
	if err != nil {
		return authz.Subject{}, err
	}

	id, err := w.resolve(subject.ID)
	if err != nil {
		return authz.Subject{}, err
	}

	subject.ID = id

	return subject, nil
}

// resolveObject parses and resolves an object from the file.
func (w *world) resolveObject(s string) (authz.Object, error) {
	object, err := authz.ParseObject(s)
	if err != nil {
		return authz.Object{}, err
	}

	id, err := w.resolve(object.ID)
	if err != nil {
		return authz.Object{}, err
	}

	object.ID = id

	return object, nil
}

// buildWorld creates the organization, users, groups and grants the spec
// describes.
func buildWorld(t *testing.T, db *store.DB, s spec) *world {
	t.Helper()

	repos, orgCtx, org := newOrg(t, db)

	w := &world{
		repos:  repos,
		ctx:    orgCtx,
		orgID:  org.ID,
		users:  make(map[string]uuid.UUID, len(s.Fixture.Users)),
		groups: make(map[string]uuid.UUID, len(s.Fixture.Groups)),
	}

	for _, name := range s.Fixture.Users {
		user, err := repos.Users.Create(orgCtx, repo.CreateUser{
			Email: name + "@example.com", Name: name, IsActive: true,
		})
		if err != nil {
			t.Fatalf("create user %s: %v", name, err)
		}

		w.users[name] = user.ID
	}

	// Two passes, because a group may name a parent declared after it.
	for _, g := range s.Fixture.Groups {
		group, err := repos.Groups.Create(orgCtx, repo.CreateGroup{Name: g.Name})
		if err != nil {
			t.Fatalf("create group %s: %v", g.Name, err)
		}

		w.groups[g.Name] = group.ID
	}

	for _, g := range s.Fixture.Groups {
		if g.Parent == "" {
			continue
		}

		parentID, ok := w.groups[g.Parent]
		if !ok {
			t.Fatalf("group %s names unknown parent %s", g.Name, g.Parent)
		}

		current, err := repos.Groups.Get(orgCtx, w.groups[g.Name])
		if err != nil {
			t.Fatalf("read group %s: %v", g.Name, err)
		}

		if _, err := repos.Groups.Update(orgCtx, repo.UpdateGroup{
			ID:            current.ID,
			Name:          current.Name,
			Description:   current.Description,
			ParentGroupID: uuid.NullUUID{UUID: parentID, Valid: true},
			ExternalID:    current.ExternalID.String,
			Version:       current.Version,
		}); err != nil {
			t.Fatalf("set parent of %s: %v", g.Name, err)
		}
	}

	for groupName, members := range s.Fixture.Members {
		groupID, ok := w.groups[groupName]
		if !ok {
			t.Fatalf("members list names unknown group %s", groupName)
		}

		for _, member := range members {
			userID, found := w.users[member]
			if !found {
				t.Fatalf("group %s names unknown user %s", groupName, member)
			}

			if err := repos.Groups.AddMember(orgCtx, groupID, userID); err != nil {
				t.Fatalf("add %s to %s: %v", member, groupName, err)
			}
		}
	}

	for _, g := range s.Fixture.Grants {
		subject, err := w.resolveSubject(g.Subject)
		if err != nil {
			t.Fatalf("grant subject: %v", err)
		}

		object, err := w.resolveObject(g.Object)
		if err != nil {
			t.Fatalf("grant object: %v", err)
		}

		subjectID, perr := uuid.Parse(subject.ID)
		if perr != nil {
			t.Fatalf("grant subject id: %v", perr)
		}

		objectID, perr := uuid.Parse(object.ID)
		if perr != nil {
			t.Fatalf("grant object id: %v", perr)
		}

		if err := repos.Roles.Grant(orgCtx, repo.GrantRole{
			SubjectType:     string(subject.Type),
			SubjectID:       subjectID,
			SubjectRelation: string(subject.Relation),
			Relation:        g.Relation,
			ObjectType:      string(object.Type),
			ObjectID:        objectID,
		}); err != nil {
			t.Fatalf("grant %s: %v", g.Subject, err)
		}
	}

	return w
}

// TestModelV1 runs every assertion in the specification against both engines.
func TestModelV1(t *testing.T) {
	t.Parallel()

	s := loadSpec(t)

	bothEngines(t, func(t *testing.T, db *store.DB) {
		w := buildWorld(t, db, s)
		checker, _ := authz.New(w.repos)

		for _, a := range s.Assertions {
			name := fmt.Sprintf("%s/%s/%s", a.Subject, a.Permission, a.Expect)

			t.Run(name, func(t *testing.T) {
				subject, err := w.resolveSubject(a.Subject)
				if err != nil {
					t.Fatalf("subject: %v", err)
				}

				object, err := w.resolveObject(a.Object)
				if err != nil {
					t.Fatalf("object: %v", err)
				}

				req := authz.Request{
					Subject:    subject,
					Permission: authz.Permission(a.Permission),
					Object:     object,
				}

				decision, err := checker.Check(w.ctx, req)
				if err != nil {
					t.Fatalf("check: %v", err)
				}

				want := a.Expect == "allow"
				if decision.Allowed != want {
					// The explanation is what makes a failure actionable:
					// "denied" alone sends someone reading the resolver.
					explanation, eerr := checker.Explain(w.ctx, req)
					if eerr != nil {
						t.Fatalf("got allowed=%v want %v (explain failed: %v)",
							decision.Allowed, want, eerr)
					}

					t.Errorf("got allowed=%v, want %v\n  subjects considered: %v\n"+
						"  relations found: %v\n  relations that would grant it: %v",
						decision.Allowed, want,
						explanation.Subjects, explanation.Relations, explanation.Granting)
				}
			})
		}
	})
}

// The assertion file must exercise every permission. A permission nobody
// asserts is a permission nobody has checked, and it will be wrong eventually.
func TestEveryPermissionIsAsserted(t *testing.T) {
	t.Parallel()

	s := loadSpec(t)

	asserted := make(map[authz.Permission]bool, len(s.Assertions))
	for _, a := range s.Assertions {
		asserted[authz.Permission(a.Permission)] = true
	}

	for _, p := range authz.AllPermissions {
		if !asserted[p] {
			t.Errorf("permission %q appears in no assertion in %s", p, modelFile)
		}
	}
}

// Every assertion must name a real permission, or a typo silently becomes an
// assertion that the misspelling is denied - which it always is.
func TestAssertionsNameRealPermissions(t *testing.T) {
	t.Parallel()

	s := loadSpec(t)

	known := make(map[authz.Permission]bool, len(authz.AllPermissions))
	for _, p := range authz.AllPermissions {
		known[p] = true
	}

	for _, a := range s.Assertions {
		if !known[authz.Permission(a.Permission)] {
			t.Errorf("assertion names unknown permission %q", a.Permission)
		}

		if a.Expect != "allow" && a.Expect != "deny" {
			t.Errorf("assertion for %q has expect=%q, want allow or deny", a.Permission, a.Expect)
		}
	}
}

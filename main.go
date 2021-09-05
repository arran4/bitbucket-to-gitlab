package main

import (
	"flag"
	"fmt"
	"github.com/ktrysmt/go-bitbucket"
	"github.com/xanzy/go-gitlab"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path"
	"strings"
)

func main() {
	flag.Parse()
	log.SetFlags(log.Flags() | log.Lshortfile)

	c := bitbucket.NewBasicAuth(bitbucket_username, bitbucket_api_token)

	gl, err := gitlab.NewClient(gitlab_api_token, gitlab.WithBaseURL(gitlab_url))
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	//listForAccount(c, bitbucket_username)
	//listTeams(c)
	//listWorkspaces(c, bitbucket_username)
	wsprojects := listWorkspacesAndProjects(c)
	missing := findMissingWorkspaces(gl, wsprojects)
	log.Printf("%#v", missing)
	createGroups(gl, missing)
	createRepos(gl, wsprojects)
	copyRepos(wsprojects)
}

func copyRepos(wsprojects []*WorkspaceProjectPair) {
	failed := []*WorkspaceProjectPair{}
	for _, wsp := range wsprojects {

		if wsp.FullRepo {
			log.Printf("Not empty")
			continue
		}

		pwd, err := os.Getwd()
		if err != nil {
			log.Printf("%v", err)
			failed = append(failed, wsp)
		}
		dir := path.Join(pwd, "t")

		srcRepo := fmt.Sprintf("https://%s@bitbucket.org/%s/%s.git", bitbucket_username, wsp.WorkspaceSlug, wsp.ProjectSlug)
		log.Printf("Git clone; %s", srcRepo)
		c := exec.Command("git", "clone", "--mirror", srcRepo, "t")
		c.Dir = pwd
		c.Stdout = os.Stdout
		c.Stdin = os.Stdin
		c.Stderr = os.Stderr
		if err := c.Start(); err != nil {
			log.Printf("%v", err)
			failed = append(failed, wsp)
		} else if err := c.Wait(); err != nil {
			log.Printf("%v", err)
			failed = append(failed, wsp)
		}
		log.Printf("Remove origin")
		c = exec.Command("git", "remote", "rm", "origin")
		c.Dir = dir
		c.Stdout = os.Stdout
		c.Stdin = os.Stdin
		c.Stderr = os.Stderr
		if err := c.Start(); err != nil {
			log.Printf("%v", err)
			failed = append(failed, wsp)
		} else if err := c.Wait(); err != nil {
			log.Printf("%v", err)
			failed = append(failed, wsp)
		}
		destRepo := fmt.Sprintf("https://%s.git", path.Join(strings.TrimPrefix(gitlab_url, "https://"), wsp.WorkspaceSlug, wsp.ProjectSlug))
		log.Printf("Git add origin; %s", destRepo)
		c = exec.Command("git", "remote", "add", "origin", destRepo)
		c.Dir = dir
		c.Stdout = os.Stdout
		c.Stdin = os.Stdin
		c.Stderr = os.Stderr
		c.Dir = dir
		if err := c.Start(); err != nil {
			log.Printf("%v", err)
			failed = append(failed, wsp)
		} else if err := c.Wait(); err != nil {
			log.Printf("%v", err)
			failed = append(failed, wsp)
		}
		log.Printf("Git push; %s", destRepo)
		c = exec.Command("git", "push", "--all")
		c.Stdout = os.Stdout
		c.Stdin = os.Stdin
		c.Stderr = os.Stderr
		c.Dir = dir
		if err := c.Start(); err != nil {
			log.Printf("%v", err)
			failed = append(failed, wsp)
		} else if err := c.Wait(); err != nil {
			log.Printf("%v", err)
			failed = append(failed, wsp)
		}
		log.Printf("Done")
		log.Printf("Deleting: %v", dir)
		if err := os.RemoveAll(dir); err != nil {
			log.Printf("%v", err)
			failed = append(failed, wsp)
		}
	}
	for _, wsp := range failed {
		log.Printf("Failed: %#v", wsp)
	}
}

func createRepos(gl *gitlab.Client, wsprojects []*WorkspaceProjectPair) {
	for _, wsp := range wsprojects {
		id := fmt.Sprintf("%s/%s", wsp.WorkspaceSlug, wsp.ProjectSlug)
		log.Printf("id: %s %s", id, url.QueryEscape(id))
		p, _, err := gl.Projects.GetProject(id, &gitlab.GetProjectOptions{})
		if err != nil {
			if strings.Contains(err.Error(), "404 Project Not Found") {
				log.Printf("Not found creating")
			} else if strings.Contains(err.Error(), "404 failed to parse unknown error format") {
				log.Printf("NFI")
			} else {
				panic(err)
			}
		}
		if p == nil {
			log.Printf("Creating")
			_, _, err := gl.Projects.CreateProject(&gitlab.CreateProjectOptions{
				Name:        gitlab.String(wsp.ProjectSlug),
				NamespaceID: wsp.NamespaceId,
				Visibility:  gitlab.Visibility(gitlab.PrivateVisibility),
			})
			if err != nil {
				if strings.Contains(err.Error(), "has already been taken") {
					continue
				}
				panic(err)
			}
		} else {
			log.Printf("full empty status: %v", p.EmptyRepo)
			wsp.FullRepo = !p.EmptyRepo
		}
	}
}

func createGroups(gl *gitlab.Client, missing []string) {
	for _, g := range missing {
		log.Printf("Creating %s", g)
		_, _, err := gl.Groups.CreateGroup(&gitlab.CreateGroupOptions{
			Name:       gitlab.String(g),
			Path:       gitlab.String(g),
			Visibility: gitlab.Visibility(gitlab.PrivateVisibility),
		})
		if err != nil {
			if strings.Contains(err.Error(), "has already been taken") {
				continue
			}
			panic(err)
		}
	}
}

func findMissingWorkspaces(gl *gitlab.Client, wsprojects []*WorkspaceProjectPair) []string {
	missing := map[string]*WorkspaceProjectPair{}
	for _, wsp := range wsprojects {
		missing[wsp.WorkspaceSlug] = wsp
	}
	u, _, err := gl.Users.CurrentUser()
	if err != nil {
		panic(err)
	}
	log.Printf("%v", u.Username)
	delete(missing, u.Username)

	namespaces, _, err := gl.Namespaces.ListNamespaces(&gitlab.ListNamespacesOptions{})
	if err != nil {
		panic(err)
	}
	for _, n := range namespaces {
		log.Printf("Group: %s", n.Name)
		if wsp, ok := missing[n.Name]; ok {
			wsp.NamespaceId = &n.ID
		}
		delete(missing, n.Name)
	}
	//groups, _, err := gl.Groups.ListGroups(&gitlab.ListGroupsOptions{
	//	AllAvailable:   gitlab.Bool(true),
	//	MinAccessLevel: gitlab.AccessLevel(gitlab.DeveloperPermissions),
	//	//Owned: gitlab.Bool(false),
	//})
	//if err != nil {
	//	panic(err)
	//}
	//for _, g := range groups {
	//	log.Printf("Group: %s", g.Name)
	//	delete(missing, g.Name)
	//}
	result := []string{}
	for k := range missing {
		result = append(result, k)
	}
	return result
}

type WorkspaceProjectPair struct {
	WorkspaceSlug string
	NamespaceId   *int
	ProjectSlug   string
	WorkspaceUUID string
	FullRepo      bool
}

func listWorkspacesAndProjects(c *bitbucket.Client) []*WorkspaceProjectPair {
	res, err := c.Workspaces.List()
	if err != nil {
		panic(err)
	}
	//res, err := c.Workspaces.List()
	//if err != nil {
	//	panic(err)
	//}
	log.Printf("Got %d", len(res.Workspaces))
	log.Printf("Size %d", res.Size)
	log.Printf("PageLen %d", res.Pagelen)

	result := []*WorkspaceProjectPair{}

	for i, w := range res.Workspaces {
		log.Printf("%d: %s type %s", i, w.Slug, w.Type)
		rres, err := c.Repositories.ListForAccount(&bitbucket.RepositoriesOptions{
			Owner: w.Slug,
		})
		if err != nil {
			panic(err)
		}

		for _, r := range rres.Items {
			log.Printf("Has repo      : %s", r.Slug)
			result = append(result, &WorkspaceProjectPair{
				WorkspaceSlug: w.Slug,
				WorkspaceUUID: w.UUID,
				ProjectSlug:   r.Slug,
			})
		}
	}
	return result
}

func listWorkspaces(c *bitbucket.Client, o string) {
	res, err := c.Workspaces.List()
	if err != nil {
		panic(err)
	}
	log.Printf("Got %d", len(res.Workspaces))
	log.Printf("Size %d", res.Size)
	log.Printf("PageLen %d", res.Pagelen)

	for i, w := range res.Workspaces {
		log.Printf("%d: %s type %s", i, w.Slug, w.Type)
		rres, err := c.Repositories.ListForAccount(&bitbucket.RepositoriesOptions{
			Owner: w.Slug,
		})
		if err != nil {
			panic(err)
		}

		for _, r := range rres.Items {
			log.Printf("Has repo      : %s", r.Slug)
		}
	}
}

func listTeams(c *bitbucket.Client) {
	res, err := c.Teams.List("admin")
	if err != nil {
		panic(err)
	}
	log.Printf("%s", res)
}

func listForAccount(c *bitbucket.Client, o string) {
	res, err := c.Repositories.ListForAccount(&bitbucket.RepositoriesOptions{
		Owner: o,
	})
	if err != nil {
		panic(err)
	}

	log.Printf("Got %d", len(res.Items))
	log.Printf("Size %d", res.Size)
	log.Printf("PageLen %d", res.Pagelen)

	for i, r := range res.Items {
		log.Printf("%d: %s (%s) from %s %s", i, r.Name, r.Slug, r.Project.Name, r.Full_name)
	}
}

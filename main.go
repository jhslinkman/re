// Partially derived from github.com/rsc/github.

package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-github/github"
	"golang.org/x/oauth2"
)

var (
	project      = flag.String("p", "", "GitHub owner/repo name")
	resume       = flag.String("resume", "", "resume review from `file`")
	tokenFile    = flag.String("token", "", "read GitHub token personal access token from `file` (default $HOME/.github-issue-token)")
	// TODO: Global?!
	projectOwner = ""
	projectRepo  = ""
)

func usage() {
	fmt.Fprintf(os.Stderr, `usage: re [-p owner/repo] [-resume file] [pr-number]

`)
	flag.PrintDefaults()
	os.Exit(2)
}

func main() {
	flag.Usage = usage
	flag.Parse()
	q := strings.Join(flag.Args(), " ")

	f := strings.Split(*project, "/")
	if len(f) > 0 {
		projectOwner = f[0]
	}
	if len(f) > 1 {
		projectRepo = f[1]
	}

	loadAuth()

	ctx := context.Background()

	n, _ := strconv.Atoi(q)
	if n != 0 {
		var filename string
		if *resume != "" {
			filename = *resume
		} else {
			filename = makeReviewTemplate(ctx, n)
		}

		request := review(n, filename)
		postComments(ctx, n, request)
	} else {
		user := loadUser()
		p := searchParams{
			user: user,
		}
		fmt.Println(projectRepo)
		mine, others, err := searchPRs(ctx, p)
		if err != nil {
			log.Fatal(err)
		}
		printIssues(mine)
		printIssues(others)
	}
	}

func printIssues(issues []*github.Issue) {
	for _, issue := range issues {
		fmt.Printf(
			"%s - %s - %s\n",
			getUserLogin(issue.User),
			getIssueFromURL(issue.URL),
			getString(issue.Title),
		)
	}
}

func getIssueFromURL(url *string) string {
	re := regexp.MustCompile(`repos/(?P<owner>[^/]+)/(?P<repo>[^/]+)/issues/(?P<number>[0-9]+)`)
	matches := re.FindStringSubmatch(getString(url))
	return matches[1] + "/" + matches[2] + "#" + matches[3]
}

func postComments(ctx context.Context, pr int, review *github.PullRequestReviewRequest) error {
	fmt.Printf("Submitting review... ")
	_, _, err := client.PullRequests.CreateReview(ctx, projectOwner, projectRepo, pr, review)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return err
	}
	fmt.Printf("success.\n")
	return nil
}

func exitHappy(args ...interface{}) {
	if len(args) > 0 {
		fmt.Println(args...)
	}
	os.Exit(0)
}

func readPipe(cmd *exec.Cmd, buf *bytes.Buffer) error {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatal(fmt.Errorf("stdoutpipe: %v", err))
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		log.Fatal(fmt.Errorf("stderrpipe: %v", err))
		return err
	}
	if err := cmd.Start(); err != nil {
		log.Fatal(fmt.Errorf("cmd start: %v", err))
	}
	if _, err := buf.ReadFrom(stdout); err != nil {
		log.Fatal(fmt.Errorf("ReadFrom: %v", err))
	}
	errBuf := new(bytes.Buffer)
	if _, err := errBuf.ReadFrom(stderr); err != nil {
		log.Fatal(fmt.Errorf("ReadFrom: %v", err))
	}
	if errBuf.Len() != 0 {
		fmt.Println(errBuf)
	}
	if err := cmd.Wait(); err != nil {
		return err
	}
	return nil
}

func editFile(filename string) ([]byte, error) {
	if err := runEditor(filename); err != nil {
		return nil, err
	}
	updated, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func runEditor(filename string) error {
	ed := os.Getenv("VISUAL")
	if ed == "" {
		ed = os.Getenv("EDITOR")
	}
	if ed == "" {
		ed = "ed"
	}

	// If the editor contains spaces or other magic shell chars,
	// invoke it as a shell command. This lets people have
	// environment variables like "EDITOR=emacs -nw".
	// The magic list of characters and the idea of running
	// sh -c this way is taken from git/run-command.c.
	var cmd *exec.Cmd
	if strings.ContainsAny(ed, "|&;<>()$`\\\"' \t\n*?[#~=%") {
		cmd = exec.Command("sh", "-c", ed+` "$@"`, "$EDITOR", filename)
	} else {
		cmd = exec.Command(ed, filename)
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("invoking editor: %v", err)
	}
	return nil
}

func wrap(t string, prefix string) string {
	out := ""
	t = strings.Replace(t, "\r\n", "\n", -1)
	lines := strings.Split(t, "\n")
	for i, line := range lines {
		if i > 0 {
			out += "\n" + prefix
		}
		s := line
		for len(s) > 70 {
			i := strings.LastIndex(s[:70], " ")
			if i < 0 {
				i = 69
			}
			i++
			out += strings.TrimRight(s[:i], " ") + "\n" + prefix
			s = s[i:]
		}
		out += strings.TrimRight(s, " ")
	}
	return out
}

var client *github.Client

// GitHub personal access token, from https://github.com/settings/applications.
var authToken string

func loadAuth() {
	const short = ".github-issue-token"
	filename := filepath.Clean(os.Getenv("HOME") + "/" + short)
	shortFilename := filepath.Clean("$HOME/" + short)
	if *tokenFile != "" {
		filename = *tokenFile
		shortFilename = *tokenFile
	}
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		log.Fatal("reading token: ", err, "\n\n"+
			"Please create a personal access token at https://github.com/settings/tokens/new\n"+
			"and write it to ", shortFilename, " to use this program.\n"+
			"The token only needs the repo scope, or private_repo if you want to\n"+
			"view or edit issues for private repositories.\n"+
			"The benefit of using a personal access token over using your GitHub\n"+
			"password directly is that you can limit its use and revoke it at any time.\n\n")
	}
	fi, err := os.Stat(filename)
	if fi.Mode()&0077 != 0 {
		log.Fatalf("reading token: %s mode is %#o, want %#o", shortFilename, fi.Mode()&0777, fi.Mode()&0700)
	}
	authToken = strings.TrimSpace(string(data))
	t := &oauth2.Transport{
		Source: &tokenSource{AccessToken: authToken},
	}
	client = github.NewClient(&http.Client{Transport: t})
}

func loadUser() string {
	cmd := exec.Command("git", "config", "github.user")
	buf := bytes.NewBuffer(make([]byte, 0, 30))
	if err := readPipe(cmd, buf); err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			log.Fatal("reading github user: ", err, "\n\n",
				"Please set your GitHub username:",
				"git config --global github.user yourusername")
		}
		log.Fatal(fmt.Errorf("invoking git config github.user: %v", err))
	}
	return strings.TrimSpace(buf.String())
}

type tokenSource oauth2.Token

func (t *tokenSource) Token() (*oauth2.Token, error) {
	return (*oauth2.Token)(t), nil
}

func getInt(x *int) int {
	if x == nil {
		return 0
	}
	return *x
}

func getString(x *string) string {
	if x == nil {
		return ""
	}
	return *x
}

func getUserLogin(x *github.User) string {
	if x == nil || x.Login == nil {
		return ""
	}
	return *x.Login
}

func getTime(x *time.Time) time.Time {
	if x == nil {
		return time.Time{}
	}
	return *x
}

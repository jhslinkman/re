package main

import (
	"context"
	"fmt"
	"time"

	"github.com/google/go-github/github"
)

type searchParams struct {
	user string
	repo string
}

func searchPRs(ctx context.Context, params searchParams) ([]*github.Issue, []*github.Issue, error) {
	var mine []*github.Issue
	var theirs []*github.Issue
	q := fmt.Sprintf("type:pull-request state:open involves:%s updated:>=%s", params.user, time.Now().AddDate(0, -1, 0).Format("2006-01-02"))
	for page := 1; ; {
		res, resp, err := client.Search.Issues(ctx, q, &github.SearchOptions{
			Sort: "created",
			ListOptions: github.ListOptions{
				Page:    page,
				PerPage: 100,
			},
		})
		for i, issue := range res.Issues {
			if getUserLogin(issue.User) == params.user {
				mine = append(mine, &res.Issues[i])
			} else {
				theirs = append(theirs, &res.Issues[i])
			}
		}
		if err != nil {
			return mine, theirs, err
		}
		if resp.NextPage < page {
			break
		}
		page = resp.NextPage
	}
	return mine, theirs, nil
}

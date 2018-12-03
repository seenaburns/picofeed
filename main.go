package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mmcdole/gofeed"
	"github.com/pkg/browser"
	"github.com/pkg/errors"
)

var (
	feedsPath = flag.String("feeds", "feeds.txt", "File with newline separated urls")
	web       = flag.Bool("web", false, "Display in browser")
)

func main() {
	flag.Parse()

	feeds, err := readFeedsFile(*feedsPath)
	if err != nil {
		log.Fatal(err)
	}

	posts := fetchFeeds(feeds)

	keys := []string{}
	for k := range posts {
		keys = append(keys, k)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(keys)))

	if *web {
		renderWeb(posts, keys)
	} else {
		render(posts, keys)
	}
}

func render(postsByTime map[string]*Post, keys []string) {
	lastDate := ""
	for _, k := range keys {
		p := postsByTime[k]

		date := p.Timestamp.Format("Jan 2006")
		if date != lastDate {
			fmt.Printf("%s\n", date)
			lastDate = date
		}

		printPost(p)
	}
}

func printPost(p *Post) {
	if len(p.Title) > 70 {
		fmt.Printf("    %v\n", p.Title)
		fmt.Printf("    %70v %s\n", "", p.Link)
	} else {
		fmt.Printf("    %-70v %s\n", p.Title, p.Link)
	}
}

func renderWeb(postsByTime map[string]*Post, keys []string) {
	buf := bytes.NewBuffer(nil)
	fmt.Fprintf(buf, `<!DOCTYPE html>
<head>
<style>
body {
	margin: 0 auto;
	max-width: 800px;
	color: #888;
	font-family: -apple-system,system-ui,BlinkMacSystemFont,"Segoe UI",Roboto,"Helvetica Neue",Arial,sans-serif;
	font-size: 14px;
	line-height: 1.4em;
}
h4   {color: #000;}
a {color: #000;}
a:visited {color: #888;}
</style>
</head>
<body>
`)

	lastDate := ""
	for _, k := range keys {
		p := postsByTime[k]

		date := p.Timestamp.Format("Jan 2006")
		if date != lastDate {
			fmt.Fprintf(buf, "<h4>%s</h4>\n", date)
			lastDate = date
		}

		fmt.Fprintf(buf, "<div><a href=\"%s\">%s</a> (%s)</div>", p.Link, p.Title, p.shortFeedLink())
	}

	fmt.Fprintf(buf, `</body>
</html>`)

	err := browser.OpenReader(buf)
	if err != nil {
		log.Fatal(errors.Wrapf(err, "browser.OpenReader"))
	}
}

type Post struct {
	Title     string
	Link      string
	Timestamp *time.Time
	FeedLink  string
	FeedTitle string
}

func (p *Post) shortFeedLink() string {
	u, err := url.Parse(p.FeedLink)
	if err != nil {
		return ""
	}

	return u.Host
}

func fetchFeeds(feeds []*url.URL) map[string]*Post {
	postsByTime := make(map[string]*Post)

	var wg sync.WaitGroup
	postChan := make(chan *Post, 10000)
	for _, f := range feeds {
		wg.Add(1)
		go func(feed *url.URL) {
			defer wg.Done()
			posts, err := fetchFeed(feed)
			if err != nil {
				log.Printf("ERROR: %v", err)
				return
			}
			for _, p := range posts {
				postChan <- p
			}
		}(f)
	}
	wg.Wait()
	close(postChan)

	for p := range postChan {
		postsByTime[fmt.Sprintf("%d-%s", p.Timestamp.Unix(), p.Link)] = p
	}
	return postsByTime
}

func fetchFeed(feedUrl *url.URL) ([]*Post, error) {
	fp := gofeed.NewParser()
	feed, err := fp.ParseURL(feedUrl.String())
	if err != nil {
		return nil, errors.Wrapf(err, "gofeed.ParseURL(%q)", feedUrl.String())
	}

	posts := []*Post{}
	for _, i := range feed.Items {
		t := i.PublishedParsed
		if i.PublishedParsed == nil {
			if i.UpdatedParsed != nil {
				t = i.UpdatedParsed
			} else {
				log.Printf("Invalid time: %v", i.PublishedParsed)
				continue
			}
		}

		posts = append(posts, &Post{
			Title:     i.Title,
			Link:      i.Link,
			Timestamp: t,
			FeedTitle: feed.Title,
			FeedLink:  feedUrl.String(),
		})

	}

	fmt.Fprintf(os.Stderr, "Fetched %q: %d posts\n", feedUrl, len(feed.Items))

	return posts, nil
}

func readFeedsFile(path string) ([]*url.URL, error) {
	contents, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, errors.Wrapf(err, "ReadFile(%q)", path)
	}

	lines := strings.Split(string(contents), "\n")

	urls := []*url.URL{}
	for _, l := range lines {
		if l == "" {
			continue
		}
		u, err := url.Parse(l)
		if err != nil {
			return nil, errors.Wrapf(err, "url.Parse(%q)", l)
		}
		urls = append(urls, u)
	}

	return urls, nil
}

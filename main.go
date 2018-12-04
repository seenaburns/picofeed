package main

import (
	"bytes"
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
	flag "github.com/spf13/pflag"
)

var (
	web = flag.Bool("web", false, "Display in browser")
)

func main() {
	flag.Parse()

	feedsList := flag.Args()
	if len(feedsList) == 0 {
		fmt.Fprintf(os.Stderr, "No feed provied\n")
		fmt.Fprintf(os.Stderr, "Try `./picofeed feeds.txt` or `./picofeed http://seenaburns.com/feed.xml http://example.com/feed.xml`\n")
		os.Exit(1)
	}

	feeds := []*url.URL{}
	for _, f := range feedsList {
		newFeeds, err := parseFeedArg(f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Couldn't parse %q as a url or a file of newline separated urls: %v\n", f, err)
			os.Exit(1)
		}
		feeds = append(feeds, newFeeds...)
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
	padding: 2em 0px;
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
<h4 style="padding-bottom: 2em">Picofeed</h4>
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

// If feed is a path to a file, attempt to read it as a newline separated list of urls
// Otherwise try parsing as a url itself
func parseFeedArg(feed string) ([]*url.URL, error) {
	f, err := os.Stat(feed)
	if os.IsNotExist(err) || (err == nil && !f.Mode().IsRegular()) {
		// feed is not a file, treat as url
		u, err := url.Parse(feed)
		if err != nil {
			return nil, errors.Wrapf(err, "%q is not a file, url.Parse(%q) failed")
		}
		return []*url.URL{u}, nil
	}

	// feed is a file, read as newline separated urls
	contents, err := ioutil.ReadFile(feed)
	if err != nil {
		return nil, errors.Wrapf(err, "ReadFile(%q)", feed)
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

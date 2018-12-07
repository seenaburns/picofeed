package main

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mmcdole/gofeed"
	"github.com/pkg/browser"
	"github.com/pkg/errors"
	flag "github.com/spf13/pflag"
)

const VERSION = "1.1"
const FETCH_TIMEOUT = 10 * time.Second

var (
	html = flag.Bool("html", false, "Render feed as html to stdout")
	web  = flag.Bool("web", false, "Display feed in browser")
)

func init() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage:
  picofeed takes feed urls or files of newline separated urls

  Examples:
	picofeed feeds.txt --web
	picofeed http://seenaburns.com/feed.xml
	picofeed http://seenaburns.com/feed.xml feeds.txt http://example.com/feed.xml

  Flags:
`)
		flag.PrintDefaults()
	}

	flag.ErrHelp = errors.New("")
}

func main() {
	ctx := context.Background()

	flag.Parse()

	feedsList := flag.Args()
	if len(feedsList) == 0 {
		fmt.Fprintf(os.Stderr, "ERROR: No feed provided\n\n")
		flag.Usage()
		os.Exit(1)
	}

	if feedsList[0] == "version" {
		fmt.Fprintf(os.Stderr, "%s\n", VERSION)
		return
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

	posts := fetchAll(ctx, feeds)
	if *web {
		f, err := ioutil.TempFile("", "picoweb.*.html")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to make temp file: %v", err)
			os.Exit(1)
		}
		defer f.Close()

		renderHtml(f, posts, "Jan 2006")

		_ = browser.OpenFile(f.Name())
	} else if *html {
		renderHtml(os.Stdout, posts, "Jan 2006")
	} else {
		render(posts, "Jan 2006")
	}
}

func render(posts []*Post, dateFormat string) {
	grouped := groupByDate(posts, dateFormat)

	for _, group := range grouped {
		for i, p := range group {
			if i == 0 {
				fmt.Printf("%s\n", p.Timestamp.Format(dateFormat))
			}
			if len(p.Title) > 70 {
				fmt.Printf("    %v\n", p.Title)
				fmt.Printf("    %70v %s\n", "", p.Link)
			} else {
				fmt.Printf("    %-70v %s\n", p.Title, p.Link)
			}
		}
	}
}

func renderHtml(f io.Writer, posts []*Post, dateFormat string) {
	fmt.Fprintf(f, `<!DOCTYPE html>
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

	grouped := groupByDate(posts, dateFormat)

	for _, group := range grouped {
		for i, p := range group {
			if i == 0 {
				fmt.Fprintf(f, "<h4>%s</h4>\n", p.Timestamp.Format(dateFormat))
			}
			fmt.Fprintf(f, "<div><a href=\"%s\">%s</a> (%s)</div>\n", p.Link, p.Title, p.shortFeedLink())
		}
	}

	fmt.Fprintf(f, `</body>
</html>
`)
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

type Posts []*Post

func (posts Posts) Len() int      { return len(posts) }
func (posts Posts) Swap(i, j int) { posts[i], posts[j] = posts[j], posts[i] }

type ByTimestamp struct{ Posts }

func (posts ByTimestamp) Less(i, j int) bool {
	return posts.Posts[i].Timestamp.After(*posts.Posts[j].Timestamp)
}

// Return list of lists of posts, where each given list has the same date
// E.g. [Dec 2018 -> []*Post, Nov 2018 -> []*Post, ...]
// Mutates posts (sorts) before running
func groupByDate(posts []*Post, dateFormat string) [][]*Post {
	sort.Sort(ByTimestamp{posts})

	// Initialize with 1 list
	grouped := [][]*Post{[]*Post{}}

	lastDate := ""
	for _, p := range posts {
		date := p.Timestamp.Format(dateFormat)
		if date != lastDate {
			// New date, make new list
			grouped = append(grouped, []*Post{})
			lastDate = date
		}
		current := len(grouped) - 1
		grouped[current] = append(grouped[current], p)
	}
	return grouped
}

// Fetch list of feeds in parallel, aggregate results
func fetchAll(ctx context.Context, feeds []*url.URL) []*Post {
	ctxTimeout, timeoutCancel := context.WithTimeout(ctx, FETCH_TIMEOUT)
	defer timeoutCancel()

	var wg sync.WaitGroup
	postChan := make(chan *Post, 10000)
	for _, f := range feeds {
		wg.Add(1)
		go func(feed *url.URL) {
			defer wg.Done()

			feedData, err := fetchFeed(ctxTimeout, feed, 0)
			if err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
				return
			}

			posts, err := parseFeed(feed, feedData)
			if err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: failed reading feed data %q: %v\n", feed, err)
			}

			for _, p := range posts {
				postChan <- p
			}
		}(f)
	}
	wg.Wait()
	close(postChan)

	posts := []*Post{}
	for p := range postChan {
		posts = append(posts, p)
	}
	return posts
}

// Fetch a single feed into a list of posts
func fetchFeed(ctx context.Context, feedUrl *url.URL, depth int) (*gofeed.Feed, error) {
	feedParser := gofeed.NewParser()

	client := &http.Client{}
	req, _ := http.NewRequest("GET", feedUrl.String(), nil)
	req.Header.Set("User-Agent", fmt.Sprintf("picofeed/%s", VERSION))
	req = req.WithContext(ctx)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%d: %s", resp.StatusCode, resp.Status)
	}

	contents, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed reading response body")
	}

	feed, err := feedParser.ParseString(string(contents))
	if err == gofeed.ErrFeedTypeNotDetected && depth == 0 {
		// User possibly tried to pass in a non-feed page, try to look for link to feed in header
		// If found, recurse
		newFeed := extractFeedLink(feedUrl, string(contents))
		if newFeed == nil {
			return nil, errors.New("Feed type not recognized, could not extract feed from <head>")
		}
		fmt.Fprintf(os.Stderr, "Autodiscovering feed %q for %q\n", newFeed, feedUrl)
		return fetchFeed(ctx, newFeed, 1)
	}

	return feed, err
}

func extractFeedLink(baseUrl *url.URL, contents string) *url.URL {
	regexes := []string{
		`\s*<link.*type="application/rss\+xml.*href="([^"]*)"`,
		`\s*<link.*type="application/atom\+xml.*href="([^"]*)"`,
	}

	for _, r := range regexes {
		re := regexp.MustCompile(r)
		matches := re.FindStringSubmatch(contents)
		if len(matches) > 1 {
			if strings.HasPrefix(matches[1], "/") {
				// relative path
				newUrl := *baseUrl
				newUrl.Path = matches[1]
				return &newUrl
			}

			u, err := url.Parse(matches[1])
			if err != nil {
				fmt.Fprintf(os.Stderr, "Autodetected %q for %q but could not parse url", matches[1], baseUrl)
				continue
			}
			return u
		}
	}

	return nil
}

func parseFeed(feedUrl *url.URL, feed *gofeed.Feed) ([]*Post, error) {
	posts := []*Post{}
	for _, i := range feed.Items {
		t := i.PublishedParsed
		if i.PublishedParsed == nil {
			if i.UpdatedParsed != nil {
				t = i.UpdatedParsed
			} else {
				fmt.Fprintf(os.Stderr, "Invalid time (%q): %v", i.Title, i.PublishedParsed)
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
			return nil, errors.Wrapf(err, "%q is not a file, url.Parse() failed", feed)
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

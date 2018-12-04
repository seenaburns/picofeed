# Picofeed

Picofeed is a minimal terminal rss reader. It takes feed urls direct or files
of newline separated urls. It fetches all feeds on demand, and displays them.

Things you don't need with picofeed:

- An account
- A subscription
- Any state at all

Honestly it's like a fancy rss curl.

```
Examples:
    picofeed feeds.txt --web
    picofeed http://seenaburns.com/feed.xml
    picofeed http://seenaburns.com/feed.xml feeds.txt http://example.com/feed.xml
```

```sh
# Use whatever click to open your terminal supports, like cmd+double click in OSX's Terminal.app
./picofeed feeds.txt
```

<p align="center">
      <img alt="picofeed terminal rss" src="https://user-images.githubusercontent.com/2801344/49423749-45c6d080-f74d-11e8-8b61-18fc589bb857.png"/>
</p>

```sh
# Open in browser with clickable links (wow!)
./picofeed feeds.txt --web
```

<p align="center">
      <img alt="picofeed local browser rss" src="https://user-images.githubusercontent.com/2801344/49423747-4495a380-f74d-11e8-8452-0e2ee826166d.png"/>
</p>

#### Install

From source, with go 1.11 just run `go build`

Or there are precompiled binaries in the [releases page](https://github.com/seenaburns/picofeed/releases/latest)

#### Other

Picofeed is built on top of [gofeed](https://github.com/mmcdole/gofeed)

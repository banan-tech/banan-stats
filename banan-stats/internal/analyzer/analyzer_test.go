package analyzer

import "testing"

func TestLineAgentFeedFetcher(t *testing.T) {
	ua := "NewsBlur Feed Fetcher - 54 subscribers - https://www.newsblur.com/site/6865328/grumpy-website"
	agent := lineAgent(ua)
	if agent != "NewsBlur Feed Fetcher" {
		t.Fatalf("expected agent to be NewsBlur Feed Fetcher, got %q", agent)
	}
}

func TestLineTypeRSS(t *testing.T) {
	ua := "My RSS Reader"
	typ := lineType("", "", ua)
	if typ != "feed" {
		t.Fatalf("expected type to be feed, got %q", typ)
	}
}

func TestLineUniqFeedID(t *testing.T) {
	ua := "Feedbin feed-id:1373711 - 192 subscribers"
	agent := "Feedbin"
	uniq := lineUniq("1.2.3.4", ua, agent)
	expected := hashUUID(agent + "/1373711")
	if uniq != expected {
		t.Fatalf("expected uniq %q, got %q", expected, uniq)
	}
}

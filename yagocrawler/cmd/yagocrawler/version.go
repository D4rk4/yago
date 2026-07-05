package main

// version is yago-crawler's calendar build version (YYYY.M), the crawler's brand
// identity inside its crawl User-Agent. Release builds may override the baked-in
// version through -ldflags "-X main.version=<ver>" (see yagocrawler/Dockerfile);
// it never affects wire compatibility, which relies on YaCy protocol values only.
var version = "2026.7"

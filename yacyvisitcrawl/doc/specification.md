# yacyvisitcrawl — Technical Specification

## Context

Nothing in the stack observes which pages a person actually visits, so visited pages
never become fresh crawl work. `yacyvisitcrawl` is a standalone, disposable Go service
that closes this gap: it receives a request naming a page someone visited, turns that
visit into one crawl order onto the broker `yacycrawler` consumes from, and sends the
browser on to the page. A link issuer that routes visits through the service is the
typical source, but the service serves any `yacycrawler` operator, whatever their pages
are searched with.

## Non-Goals

* Fetching, parsing, ranking, or storing the visited page.
* Producing search results or knowing how the visit request was formed.
* Coupling to any particular search frontend or result source.
* Identifying or authenticating the person who visited the page.
* Guaranteeing that a visit ever yields a crawl order.
* Persisting a history of visits.

## Functional Requirements

* The service SHALL accept a request naming one visited page.
* The service SHALL attempt to place one crawl order for the visited page onto the broker.
* The service SHALL redirect the browser to the visited page whether or not the crawl order
  was placed.
* The service SHALL NOT delay the redirect on the outcome of placing a crawl order.
* The service SHALL let operators configure the broker it places orders on and the crawl
  scope each order carries.

## Non-Functional Requirements

* The service SHALL bound the time spent placing a crawl order, independent of the
  destination page or broker health.
* The broker SHALL be replaceable behind a narrow interface, with no change to redirect
  behavior.
* The service SHALL persist no state of its own between requests.
* The service SHALL keep memory usage bounded independently of request volume, with explicit
  limits on in-flight requests and request-body size.
* Operational behavior SHALL be observable through machine-readable metrics, including the
  rate of requests whose crawl order could not be placed.

## Known Limitations

* A crawl order lost to a broker failure at the moment of a visit is not retried; the page
  is crawled again only if it is visited again.
* The same page visited repeatedly produces a crawl order each time; the service does not
  deduplicate across visits.

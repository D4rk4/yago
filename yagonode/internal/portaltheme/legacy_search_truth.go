package portaltheme

import "strings"

const legacyIncompleteSuggestionFragment = `{{else}}{{#if results.didYouMean}}
  <p class="meta" role="status">No results matched. Did you mean <a href="{{results.didYouMeanUrl}}">{{results.didYouMean}}</a>?</p>
  {{/if}}
  {{/if}}`

const currentIncompleteSuggestionFragment = `{{else}}{{#if results.incomplete}}{{else}}{{#if results.didYouMean}}
  <p class="meta" role="status">No results matched. Did you mean <a href="{{results.didYouMeanUrl}}">{{results.didYouMean}}</a>?</p>
  {{/if}}
  {{/if}}
  {{/if}}`

const legacySearchWindowFragment = `<p class="meta" role="status">` +
	`{{#if results.totalResults}}Up to {{formatNumber results.totalResults}} ` +
	`{{pluralize results.totalResults "result" "results"}} available in this search window` +
	`{{else}}0 results{{/if}} for “{{results.query}}”` +
	`{{#if elapsed}} ({{elapsed}}){{/if}}.` +
	`{{#if results.results}} On this page: {{results.localCount}} from this node · ` +
	`{{results.peerCount}} from peers · {{results.webCount}} from the web.{{/if}}` +
	`{{#if results.peersFailed}} {{results.peersFailed}} peer(s) unreachable or timed out.{{/if}}</p>`

const currentSearchWindowFragment = `{{#if results.incomplete}}` +
	`<p class="meta" role="status">Some enabled search sources were unavailable; ` +
	`{{#if results.results}}this result window may be incomplete.` +
	`{{else}}no complete result set is available. Please try again.{{/if}}` +
	`{{#if results.federationUnavailable}} Peer federation was unavailable for part of this search.{{/if}}` +
	`{{#if results.peersFailed}} {{results.peersFailed}} identified peer response(s) failed.{{/if}}` +
	`</p>{{/if}}
  <p class="meta" role="status">` +
	`{{#if results.totalResults}}Up to {{formatNumber results.totalResults}} ` +
	`{{pluralize results.totalResults "result" "results"}} available in this search window` +
	`{{else}}{{#if results.incomplete}}No results are currently available{{else}}0 results{{/if}}{{/if}} ` +
	`for “{{results.query}}”{{#if elapsed}} ({{elapsed}}){{/if}}.` +
	`{{#if results.results}} On this page: {{results.localCount}} from this node · ` +
	`{{results.peerCount}} from peers · {{results.webCount}} from the web.{{/if}}</p>`

func repairLegacySearchTruth(body string) string {
	body = strings.ReplaceAll(
		body,
		legacyIncompleteSuggestionFragment,
		currentIncompleteSuggestionFragment,
	)
	body = strings.ReplaceAll(body, legacySearchWindowFragment, currentSearchWindowFragment)

	return strings.ReplaceAll(
		body,
		`<fieldset><legend>{{title}}</legend>`,
		`<fieldset><legend>{{title}} — counts from {{scope}}</legend>`,
	)
}

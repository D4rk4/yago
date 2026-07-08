# SPDX-License-Identifier: AGPL-3.0-or-later

import json

about = {}
categories = ["general"]
paging = True

results_per_page = 10
elasticsearch_url = ""
elasticsearch_index = ""

_content_fragment_length = 300


def request(query, params):
    params["url"] = "{}/{}/_search".format(
        elasticsearch_url.rstrip("/"), elasticsearch_index
    )
    params["method"] = "POST"
    params["headers"]["Content-Type"] = "application/json"
    params["data"] = json.dumps(
        {
            "from": (params["pageno"] - 1) * results_per_page,
            "size": results_per_page,
            "query": {
                "multi_match": {
                    "query": query,
                    "fields": ["title^3", "content"],
                }
            },
            "highlight": {"fields": {"content": {}}},
        }
    )
    return params


def response(resp):
    try:
        hits = resp.json()["hits"]["hits"]
    except (ValueError, KeyError, TypeError):
        return []

    results = []
    for hit in hits:
        source = hit.get("_source", {})
        title = source.get("title")
        url = source.get("url")
        if not title or not url:
            continue
        results.append(
            {
                "title": title,
                "url": url,
                "content": _matched_content(hit, source),
            }
        )
    return results


def _matched_content(hit, source):
    fragments = hit.get("highlight", {}).get("content")
    if fragments:
        return " … ".join(fragments)
    return source.get("content", "")[:_content_fragment_length]

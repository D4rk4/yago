# SPDX-License-Identifier: AGPL-3.0-or-later

import json

about = {}
categories = ["general"]
paging = True

results_per_page = 10
search_index_engine = ""
elasticsearch_url = ""
elasticsearch_index = ""
manticore_url = ""
manticore_table = ""

_content_fragment_length = 300
_title_weight = 3


def request(query, params):
    params["method"] = "POST"
    params["headers"]["Content-Type"] = "application/json"
    if search_index_engine == "manticore":
        _manticore_request(query, params)
    elif search_index_engine == "elasticsearch":
        _elasticsearch_request(query, params)
    else:
        raise ValueError("unknown search_index_engine: {}".format(search_index_engine))
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


def _elasticsearch_request(query, params):
    params["url"] = "{}/{}/_search".format(
        elasticsearch_url.rstrip("/"), elasticsearch_index
    )
    params["data"] = json.dumps(
        {
            "from": _result_offset(params),
            "size": results_per_page,
            "query": {
                "multi_match": {
                    "query": query,
                    "fields": ["title^{}".format(_title_weight), "content"],
                }
            },
            "highlight": {
                "fields": {"content": {}},
                "pre_tags": [""],
                "post_tags": [""],
            },
        }
    )


def _manticore_request(query, params):
    params["url"] = "{}/search".format(manticore_url.rstrip("/"))
    params["data"] = json.dumps(
        {
            "table": manticore_table,
            "offset": _result_offset(params),
            "limit": results_per_page,
            "query": {"match": {"title,content": query}},
            "options": {
                "field_weights": {"title": _title_weight, "content": 1},
            },
            "highlight": {
                "fields": ["content"],
                "before_match": "",
                "after_match": "",
            },
        }
    )


def _result_offset(params):
    return (params["pageno"] - 1) * results_per_page


def _matched_content(hit, source):
    fragments = hit.get("highlight", {}).get("content")
    if fragments:
        return " … ".join(fragments)
    return source.get("content", "")[:_content_fragment_length]

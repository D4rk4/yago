# SPDX-License-Identifier: AGPL-3.0-or-later

import importlib
import json

import pytest

crawled_text_search = importlib.import_module("crawled_text_search")


@pytest.fixture(autouse=True)
def configured(monkeypatch):
    monkeypatch.setattr(
        crawled_text_search, "elasticsearch_url", "http://elasticsearch:9200"
    )
    monkeypatch.setattr(crawled_text_search, "elasticsearch_index", "yacy-text")


def build_params(pageno=1):
    return {"pageno": pageno, "headers": {}, "url": "", "method": "GET", "data": ""}


def test_request_targets_configured_index():
    params = crawled_text_search.request("wildflower", build_params())
    assert params["url"] == "http://elasticsearch:9200/yacy-text/_search"
    assert params["method"] == "POST"


def test_request_body_carries_multi_match_query():
    params = crawled_text_search.request("wildflower", build_params())
    body = json.loads(params["data"])
    assert body["query"]["multi_match"]["query"] == "wildflower"
    assert body["query"]["multi_match"]["fields"] == ["title^3", "content"]


def test_request_paginates_from_pageno():
    params = crawled_text_search.request("wildflower", build_params(pageno=3))
    body = json.loads(params["data"])
    assert body["from"] == 2 * crawled_text_search.results_per_page
    assert body["size"] == crawled_text_search.results_per_page


class FakeResponse:
    def __init__(self, payload):
        self._payload = payload

    def json(self):
        return self._payload


def test_response_maps_hit_to_result_with_highlight():
    resp = FakeResponse(
        {
            "hits": {
                "hits": [
                    {
                        "_source": {
                            "title": "Riverside Wildflower Guide",
                            "url": "https://example.invalid/wildflower-guide",
                            "content": "A field guide to wildflowers.",
                        },
                        "highlight": {
                            "content": ["A field guide to <em>wildflowers</em>."]
                        },
                    }
                ]
            }
        }
    )
    results = crawled_text_search.response(resp)
    assert results == [
        {
            "title": "Riverside Wildflower Guide",
            "url": "https://example.invalid/wildflower-guide",
            "content": "A field guide to <em>wildflowers</em>.",
        }
    ]


def test_response_falls_back_to_truncated_content_without_highlight():
    resp = FakeResponse(
        {
            "hits": {
                "hits": [
                    {
                        "_source": {
                            "title": "Riverside Wildflower Guide",
                            "url": "https://example.invalid/wildflower-guide",
                            "content": "A field guide to wildflowers.",
                        }
                    }
                ]
            }
        }
    )
    results = crawled_text_search.response(resp)
    assert results[0]["content"] == "A field guide to wildflowers."


def test_response_skips_hit_missing_title_or_url():
    resp = FakeResponse(
        {"hits": {"hits": [{"_source": {"content": "no title or url"}}]}}
    )
    assert crawled_text_search.response(resp) == []


def test_response_returns_empty_list_on_malformed_body():
    assert crawled_text_search.response(FakeResponse({"unexpected": "shape"})) == []


def test_response_returns_empty_list_when_json_raises():
    class RaisingResponse:
        def json(self):
            raise ValueError("not json")

    assert crawled_text_search.response(RaisingResponse()) == []

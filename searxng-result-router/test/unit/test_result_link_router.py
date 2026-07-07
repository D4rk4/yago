# SPDX-License-Identifier: AGPL-3.0-or-later

import importlib

import pytest

from searx.plugins import PluginCfg

result_link_router = importlib.import_module("result_link_router")


class FakeResult:
    def __init__(self, **fields: str) -> None:
        self._fields = dict(fields)
        self.filter_urls_calls = 0

    def filter_urls(self, filter_func) -> None:
        self.filter_urls_calls += 1
        for field_name, url_src in list(self._fields.items()):
            new_url = filter_func(self, field_name, url_src)
            if isinstance(new_url, str):
                self._fields[field_name] = new_url

    def __getitem__(self, field_name: str) -> str:
        return self._fields[field_name]


@pytest.fixture
def plugin(monkeypatch):
    monkeypatch.setenv("YACYVISITCRAWL_BASE_URL", "http://yacyvisitcrawl:8091")
    return result_link_router.SXNGPlugin(PluginCfg(active=True))


def test_rewrites_http_url(plugin):
    rewritten = plugin.route_through_visitcrawl(None, "url", "http://example.com/a")
    assert (
        rewritten == "http://yacyvisitcrawl:8091/visit?url=http%3A%2F%2Fexample.com%2Fa"
    )


def test_rewrites_https_url(plugin):
    rewritten = plugin.route_through_visitcrawl(
        None, "url", "https://example.com/a?b=c"
    )
    assert (
        rewritten
        == "http://yacyvisitcrawl:8091/visit?url=https%3A%2F%2Fexample.com%2Fa%3Fb%3Dc"
    )


def test_leaves_non_url_field_unchanged(plugin):
    assert (
        plugin.route_through_visitcrawl(None, "img_src", "http://example.com/a.png")
        is True
    )


def test_leaves_non_http_scheme_unchanged(plugin):
    assert plugin.route_through_visitcrawl(None, "url", "ftp://example.com/a") is True


def test_respects_configured_base_url(monkeypatch):
    monkeypatch.setenv("YACYVISITCRAWL_BASE_URL", "https://visitcrawl.internal:9443/")
    configured = result_link_router.SXNGPlugin(PluginCfg(active=True))
    rewritten = configured.route_through_visitcrawl(
        None, "url", "https://example.com/a"
    )
    assert (
        rewritten
        == "https://visitcrawl.internal:9443/visit?url=https%3A%2F%2Fexample.com%2Fa"
    )


def test_requires_base_url_configured(monkeypatch):
    monkeypatch.delenv("YACYVISITCRAWL_BASE_URL", raising=False)
    with pytest.raises(ValueError):
        result_link_router.SXNGPlugin(PluginCfg(active=True))


def test_on_result_rewrites_url_and_keeps_result(plugin):
    result = FakeResult(
        url="https://example.com/a", img_src="https://example.com/a.png"
    )

    kept = plugin.on_result(request=None, search=None, result=result)

    assert kept is True
    assert result.filter_urls_calls == 1
    assert (
        result["url"]
        == "http://yacyvisitcrawl:8091/visit?url=https%3A%2F%2Fexample.com%2Fa"
    )
    assert result["img_src"] == "https://example.com/a.png"

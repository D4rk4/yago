# SPDX-License-Identifier: AGPL-3.0-or-later

import os
import typing as t
from urllib.parse import quote

from searx.plugins import Plugin, PluginInfo

if t.TYPE_CHECKING:
    from searx.extended_types import SXNG_Request
    from searx.plugins import PluginCfg
    from searx.result_types import LegacyResult, Result
    from searx.search import SearchWithPlugins


class SXNGPlugin(Plugin):
    id = "result_link_router"

    def __init__(self, plg_cfg: "PluginCfg") -> None:
        super().__init__(plg_cfg)
        self.info = PluginInfo(
            id=self.id,
            name="Result link router",
            description="Route result links through yacyvisitcrawl before their destination",
            preference_section="privacy",
        )
        base_url = os.environ.get("YACYVISITCRAWL_BASE_URL")
        if not base_url:
            raise ValueError("YACYVISITCRAWL_BASE_URL must be set")
        self.visitcrawl_base_url = base_url.rstrip("/")

    def on_result(
        self, request: "SXNG_Request", search: "SearchWithPlugins", result: "Result"
    ) -> bool:
        result.filter_urls(self.route_through_visitcrawl)
        return True

    def route_through_visitcrawl(
        self, result: "Result | LegacyResult", field_name: str, url_src: str
    ) -> bool | str:
        if field_name != "url" or not url_src.startswith(("http://", "https://")):
            return True
        return f"{self.visitcrawl_base_url}/visit?url={quote(url_src, safe='')}"

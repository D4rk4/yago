# SPDX-License-Identifier: AGPL-3.0-or-later

import dataclasses
import sys
import types
import typing as t


def _install_searx_plugins_stub() -> None:
    searx_pkg = types.ModuleType("searx")
    plugins_pkg = types.ModuleType("searx.plugins")

    @dataclasses.dataclass
    class PluginCfg:
        active: bool = False

    @dataclasses.dataclass
    class PluginInfo:
        id: str
        name: str
        description: str
        preference_section: t.Optional[str] = "general"
        examples: list = dataclasses.field(default_factory=list)
        keywords: list = dataclasses.field(default_factory=list)

    class Plugin:
        id: str = ""

        def __init__(self, plg_cfg: "PluginCfg") -> None:
            self.active = plg_cfg.active

        def on_result(self, request: object, search: object, result: object) -> bool:
            return True

    plugins_pkg.Plugin = Plugin  # type: ignore[attr-defined]
    plugins_pkg.PluginInfo = PluginInfo  # type: ignore[attr-defined]
    plugins_pkg.PluginCfg = PluginCfg  # type: ignore[attr-defined]
    searx_pkg.plugins = plugins_pkg  # type: ignore[attr-defined]

    sys.modules["searx"] = searx_pkg
    sys.modules["searx.plugins"] = plugins_pkg


_install_searx_plugins_stub()

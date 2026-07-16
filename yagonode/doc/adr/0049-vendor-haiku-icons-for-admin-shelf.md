# 49. Vendor Haiku icons for the Admin shelf

Date: 2026-07-16

## Status

Accepted

## Context

The compact Photon Admin shelf needs small, colorful icons that remain legible
against inactive, hover, and selected row backgrounds. Copying QNX artwork is
not acceptable, monochrome `currentColor` symbols lose their identity when a
row changes color, and a remote icon service would violate the console's
offline and strict-CSP requirements.

## Decision

Vendor only the fifteen SVG assets used by the console's fourteen shelf rows and System Monitor heading from Haiku Icons
release `v1.2`, commit `ba4ad17e120b50c87d22ae5127f044257bbbf257`, published
at `https://github.com/lxmx/haiku-icons`. The upstream license is MIT/X
Consortium, copyright 2021 phillbush; its exact text is retained beside the
assets as `HAIKU-ICONS-LICENSE.txt`.

The selected SVG files and source SHA-256 values are:

| Shelf entry | Source file | SHA-256 |
| --- | --- | --- |
| Overview | `desktop.svg` | `77edf404b355cd95ab86eebe89620bd9a2082f4ab37333c69569e4a28a0f75aa` |
| Search | `system-search.svg` | `8dadf8c346529d0a2682a7037d1af5049c84c9c9b20c8453ffc5b01ce0856bf1` |
| Activity | `appointment.svg` | `afa83755d6ef9d39c8e47a6668309d05bede49fae28a5dd669d12c4e8c093ee1` |
| Public portal | `applications-internet.svg` | `b5d5c68cb0ca53dbc92827e9b362f62e155632a4a12ce5dc329b8a24dbca1343` |
| Crawler | `gnome-robots.svg` | `1e0394b41df50e42f654bfbc518683a8af8b9f08e3b19161e037ea741da3cc0d` |
| Network | `network-workgroup.svg` | `db511085881e4e9da8a4b35d23661d6ac4c10b58dfcd93dab000ed076e3b3ebe` |
| Index | `drive-partition.svg` | `750042735fbeb5ffcbb31aabd33f0c71a7eb101ba10783e10173892872aca8d7` |
| YagoRank | `applications-science.svg` | `9927035c98c9644b08fe8c3b3f39e61646554a5992e652238e97504e27dc3970` |
| Performance | `speedometer.svg` | `3f0f79ca4859bdb01a638e96e08ef1da07d486e9b18b2e8f263e8bd528f12350` |
| System Monitor heading | `utilities-system-monitor.svg` | `3c09dc945b3b1cdf21084d9483ffb3b1a8853772528dbcb8a5e9cdfdc7b8e175` |
| Backup | `media-floppy.svg` | `964bd85894630c65c57c9ec61442af6f4359787708e3c98ae00cd0d544073afa` |
| Configuration | `preferences-system.svg` | `dfc855802cd58550c0b9cb736108813c11889c0599b1f4f3f7830f82a63cad48` |
| Security | `security-high.svg` | `7f7a182f456934e8f238ed52d0b13e8e5c7848f8a177a98cf4f46fdd43cd3db6` |
| Logs | `accessories-text-editor.svg` | `fe1e9872e7ca3ae23e23b5e17b7702121ce1edbeb82ebb6f56ac7f5d2a7b0122` |
| Restart | `view-refresh.svg` | `0f05dcf9e7147aa40f5f06f395a8aedb8bad1be33d10362d2fc8c4b7050b1542` |

The SVGs are embedded and served through the existing content-digest Admin
asset catalog. Text labels remain the accessible navigation names; icon images
have empty alternative text. CSS does not recolor or filter them.

## Considered alternatives

The prior inline monochrome SVG sprite was rejected because its icons become a
single foreground color. Raster icon sets were rejected because the selected
Haiku source scales natively and is closer to the requested desktop treatment.
QNX artwork was rejected because the visual reference does not grant permission
to redistribute its assets. Remote icon CDNs were rejected because the Admin
console must work offline.

## Consequences

The shelf and System Monitor title retain detailed color at every row state and
need no runtime network access. The embedded binary gains fifteen bounded SVG
files plus the license text. Updating or replacing them requires a new pinned
source review, checksum update, and cross-browser screenshot audit.

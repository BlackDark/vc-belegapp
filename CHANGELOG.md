# Changelog

## [1.2.0](https://github.com/BlackDark/vc-belegapp/compare/v1.1.1...v1.2.0) (2026-10-20)


### Features

* **auth:** support wildcard patterns in the OIDC allowlist ([#33](https://github.com/BlackDark/vc-belegapp/issues/33)) ([3369f3a](https://github.com/BlackDark/vc-belegapp/commit/3369f3aa00341562f7166dcdf292b308d6fa965c))
* **images:** list unassigned images for review ([a41bb9d](https://github.com/BlackDark/vc-belegapp/commit/a41bb9de422a1db3ef44fa7ffd9120f7b9a67d57))
* **mail:** ingest receipt images from an IMAP mailbox ([a629028](https://github.com/BlackDark/vc-belegapp/commit/a62902850aac0453dae876ab161a1603446cc33c))
* **notify:** post a webhook when an image needs review ([9f219db](https://github.com/BlackDark/vc-belegapp/commit/9f219db03ecc74b25db5729e0d40546a64b8116c))
* **values:** serve official values from the API ([a05678e](https://github.com/BlackDark/vc-belegapp/commit/a05678e9ee1f648cc914a64508521332fc21f4a5))
* **watch:** ingest receipts from a watched folder ([6acbb53](https://github.com/BlackDark/vc-belegapp/commit/6acbb53c29993ed154ebe615525b119fe55d0dda))
* **web:** make the screens work on phones and wide screens ([#51](https://github.com/BlackDark/vc-belegapp/issues/51)) ([f6fe0b4](https://github.com/BlackDark/vc-belegapp/commit/f6fe0b4f8e7ba0f26a5beebb814e93e673198c7d))


### Bug Fixes

* **auth:** put the instance password in the database and support trusted headers ([#50](https://github.com/BlackDark/vc-belegapp/issues/50)) ([8029ef0](https://github.com/BlackDark/vc-belegapp/commit/8029ef0e7c7e6e7e424577b338119dbbcf467be7))
* **ci:** keep the tool PATH when the pdf tool cache hits ([8438826](https://github.com/BlackDark/vc-belegapp/commit/843882600c2b566478416117140f9bc59a7538ef))
* **config:** require a mail password and stop leaking mailbox details ([bf311ba](https://github.com/BlackDark/vc-belegapp/commit/bf311ba778781e9d74ad897aef369eeda7aa5c5f))
* **config:** validate job retention and allow per-type job deadlines ([94fa6d9](https://github.com/BlackDark/vc-belegapp/commit/94fa6d9727238b8e4b83fe2be36211d73647192f))
* **db:** convert an existing database to incremental auto_vacuum on startup ([#43](https://github.com/BlackDark/vc-belegapp/issues/43)) ([b70ee40](https://github.com/BlackDark/vc-belegapp/commit/b70ee4065b715e1a631be716b73d94de663f4b64))
* **db:** reopen after a failed replace and serialise the job lifecycle ([#35](https://github.com/BlackDark/vc-belegapp/issues/35)) ([8f62c9f](https://github.com/BlackDark/vc-belegapp/commit/8f62c9f683f0fa0583647e16d2e9c6b5057d6c21))
* **deps:** update golang.org/x/net to 0.61.0 for CVE-2026-78669 ([ed7c309](https://github.com/BlackDark/vc-belegapp/commit/ed7c3097fb31d576583d72f85d34af02fb196e67))
* **jobs:** reserve a worker for the uncapped types ([#41](https://github.com/BlackDark/vc-belegapp/issues/41)) ([404d786](https://github.com/BlackDark/vc-belegapp/commit/404d7860cfc9f4c50ac01dbba8d1063423dd3032))
* **jobs:** stop a stuck intake job from starving recognition ([964f99b](https://github.com/BlackDark/vc-belegapp/commit/964f99ba442db3e1c9eb3531e068f97d7d822ca7))
* **mail:** store a message at most once and bound one message's decode ([#36](https://github.com/BlackDark/vc-belegapp/issues/36)) ([fb294dd](https://github.com/BlackDark/vc-belegapp/commit/fb294dda9e3189c4b3b19e489568a6967236751d))
* **release:** switch archiving off instead of leaving the key out ([#49](https://github.com/BlackDark/vc-belegapp/issues/49)) ([22c3673](https://github.com/BlackDark/vc-belegapp/commit/22c36739df0fdfa142f5c6c946ba7dae0b3982ba))
* **rules:** a fresh year rule deducts no church tax ([#40](https://github.com/BlackDark/vc-belegapp/issues/40)) ([2ea93a7](https://github.com/BlackDark/vc-belegapp/commit/2ea93a77fb9984bd8d774d2410c1c6cb9169bc6a))
* **rules:** derive the maximum grant from the meal's own Sachbezugswert ([#30](https://github.com/BlackDark/vc-belegapp/issues/30)) ([48b3144](https://github.com/BlackDark/vc-belegapp/commit/48b31441f4ad4f8f4e8fd454de5d954501b8dda6))
* surface the failures that were being swallowed ([#38](https://github.com/BlackDark/vc-belegapp/issues/38)) ([9386d8a](https://github.com/BlackDark/vc-belegapp/commit/9386d8ab6fb78aea2966c6a50aa5dbdfc84da21e))


### Performance Improvements

* **ci:** cache the Go build per job, skip the duplicate release build ([#42](https://github.com/BlackDark/vc-belegapp/issues/42)) ([48a5066](https://github.com/BlackDark/vc-belegapp/commit/48a5066682101df241dfe9b7ba1ddb2e601a3729))
* **db:** index the blob and job lookups and keep statistics fresh ([5bab499](https://github.com/BlackDark/vc-belegapp/commit/5bab499d88a3121ab84e3852b3ebad83301f5319))
* **jobs:** prune finished job rows and reclaim database space ([364c527](https://github.com/BlackDark/vc-belegapp/commit/364c5275d18df5ee75e28cc4e069b0c867157c78))
* **service:** batch the expiry sweep, batch the duplicate lookup, tighten the tests ([#26](https://github.com/BlackDark/vc-belegapp/issues/26)) ([c487e55](https://github.com/BlackDark/vc-belegapp/commit/c487e5579d81800bb2853f79a064d500c75ae39b))

## [1.1.1](https://github.com/BlackDark/vc-belegapp/compare/v1.1.0...v1.1.1) (2026-10-09)


### Bug Fixes

* **ci:** stop zero-job failures on release-please pull requests ([#19](https://github.com/BlackDark/vc-belegapp/issues/19)) ([bc06b27](https://github.com/BlackDark/vc-belegapp/commit/bc06b274bff21d698142b3acb3bce1b7ae0f4bf0))

## [1.1.0](https://github.com/BlackDark/vc-belegapp/compare/v1.0.2...v1.1.0) (2026-10-09)


### Features

* **web:** redesign the UI with shadcn components and dark mode by default ([#17](https://github.com/BlackDark/vc-belegapp/issues/17)) ([cf71585](https://github.com/BlackDark/vc-belegapp/commit/cf71585e42bb73a35567c2881caca58733d2591c))

## [1.0.2](https://github.com/BlackDark/vc-belegapp/compare/v1.0.1...v1.0.2) (2026-10-09)


### Bug Fixes

* **release:** install release tools outside the checkout ([#15](https://github.com/BlackDark/vc-belegapp/issues/15)) ([2c95c34](https://github.com/BlackDark/vc-belegapp/commit/2c95c3462d02187f60677d6ece4214bfdd907277))

## [1.0.1](https://github.com/BlackDark/vc-belegapp/compare/v1.0.0...v1.0.1) (2026-10-09)


### Bug Fixes

* **ci:** run gh workflow dispatches with an explicit repo ([#12](https://github.com/BlackDark/vc-belegapp/issues/12)) ([0778c0c](https://github.com/BlackDark/vc-belegapp/commit/0778c0ccfd5c3009596b0a5c20715d5a203e84eb))
* **web:** show the whole receipt in the Heute preview ([#13](https://github.com/BlackDark/vc-belegapp/issues/13)) ([eb3739c](https://github.com/BlackDark/vc-belegapp/commit/eb3739cd6bb14a596f6f74c0c6f9fcb43dcf8b33))

## 1.0.0 (2026-10-09)


### Features

* add receipt recognition via OpenAI-compatible API (M2 Belegerkennung) ([#3](https://github.com/BlackDark/vc-belegapp/issues/3)) ([c26e149](https://github.com/BlackDark/vc-belegapp/commit/c26e149d396cdc0a2c380ae463bfd5d44ae5fac2))
* add the M0 foundation ([#1](https://github.com/BlackDark/vc-belegapp/issues/1)) ([7d13dbf](https://github.com/BlackDark/vc-belegapp/commit/7d13dbfd6195f6e26a26fa6998a24249a1094ac4))
* close the remaining v1 spec gaps ([#10](https://github.com/BlackDark/vc-belegapp/issues/10)) ([1b9ad32](https://github.com/BlackDark/vc-belegapp/commit/1b9ad3239c107fa06865ce1bbad8db1f6af2a9aa))
* data export/import and v1.0 release pipeline (M4) ([#6](https://github.com/BlackDark/vc-belegapp/issues/6)) ([e3ffb79](https://github.com/BlackDark/vc-belegapp/commit/e3ffb79b5162f2f4b7dd50b73c7afc1eec741fa4))
* M1 Erfassen (MVP) ([#2](https://github.com/BlackDark/vc-belegapp/issues/2)) ([78269eb](https://github.com/BlackDark/vc-belegapp/commit/78269ebf4cc7e1db09eba5aff47411690470d530))
* monthly PDF export and month lock (M3 Monatsexport) ([#5](https://github.com/BlackDark/vc-belegapp/issues/5)) ([bfa18cd](https://github.com/BlackDark/vc-belegapp/commit/bfa18cd7e02317681121d8b85dd1f5c0c275d96f))

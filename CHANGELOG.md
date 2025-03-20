# Changelog

This document outlines major changes between releases.

## [Unreleased]

### Added

### Fixed

### Changed
- Go 1.21+ is required to build now (#61)
- Replaced `math/rand.Read` with `math/rand/v2.ChaCha8.Read`

## [0.1.2] - 2024-03-11

### Added
- Support for zero-size objects for upload (#80) 

### Changed
- `bbolt`: Disabled syncing the DB in the object registry (#67)
- Bump `golang.org/x/net` from 0.15.0 to 0.17.0 (#69)
- Bump `google.golang.org/grpc` from 1.58.0 to 1.58.3 (#70)
- Bump `golang.org/x/crypto` from 0.14.0 to 0.17.0 (#71)
- Upgraded Go version to a minimum 1.20 and updated versions for GitHub Actions and workflows (#77, #78, #79)


## Older versions

Please refer to [GitHub releases](https://github.com/nspcc-dev/xk6-neofs/releases/) for older releases.

[0.1.2]: https://github.com/nspcc-dev/xk6-neofs/compare/v0.1.1...v0.1.2
[Unreleased]: https://github.com/nspcc-dev/xk6-neofs/compare/v0.1.2...master

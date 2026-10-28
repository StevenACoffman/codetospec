# Native PHP extractor

codetospec's first native extractor, proof of the `codetospec/facts/v1` protocol.
It uses [nikic/php-parser](https://github.com/nikic/PHP-Parser) v5, the AST
parser of the PHP ecosystem (the foundation of PHPStan and Rector).

## Installation

```sh
cd extractors/php
composer install
```

## What it extracts

1. **Static AST** (always): namespaces, classes (extends, implements,
   public methods, exact lines), `Route::<verb>(...)` calls (`route`
   facts, certainty `static`), `Schema::create('<table>', ...)` blocks with
   `$table-><type>('<name>')` columns (`table` facts).
2. **Runtime introspection** (option `--boot`, if an executable `artisan` is
   present): `php artisan route:list --json`; the routes obtained replace
   the static routes (certainty `proved`). If the boot fails, a warning
   on stderr and the static routes are kept.

Stdout output: `{"schema": "codetospec/facts/v1", "facts": [...]}`.

## codetospec configuration

In the analyzed repository's `codetospec.yaml`:

```yaml
extractors:
  - name: php
    cmd: php
    args: [extractors/php/extract.php, --root, "{src}"]
    timeout: 300s
```

Add `--boot` to the args to enable runtime introspection.
The Go binary knows nothing of this script: it knows only the protocol.

# basedpaste

A highly based pastebin and link shortener, written in Go to replace [shhrink].

~~I run a personal instance at [https://bp.tjf.sh](https://bp.tjf.sh),
which is read-only for unauthenticated users.~~
Host your own, if you really want to.

## Installation

Compile basedpaste:

```
$ make
```

Install for the current user:

```
$ make install
```

## Configuration

Create a TOML file with the following fields:

|Field          |Type   |
|---------------|-------|
|`URL`            |String |
|`Host`           |String |
|`Port`           |Integer|
|`IndexPath`      |String |
|`DbPath`         |String |
|`UploadsDir`     |String |
|`MaxFileBytes`   |Integer|
|`RequireAuth`    |Boolean|

See `config.toml.example` to get an idea of what this looks like.

If `RequireAuth` is set to `true`, all POST requests will require an API key
as a second form-field. Manually add rows to the auth table in the database
however you see fit.

## Execution

Run with the configuration at `~/.config/basedpaste/config.toml`:

```
$ basedpaste
```

Or run with an alternative config file:

```
$ basedpaste -c /path/to/config
```

It is recommended to run basedpaste with a service manager and behind a reverse proxy.

[shhrink]: https://github.com/tfaughnan/shhrink

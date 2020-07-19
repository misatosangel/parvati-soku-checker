# parvati-soku-checker
Golang command line and daemon for hisoutensoku checking.

## Overview

This package provides two fairly simple tools around histouensoku game checking
integrating it with Parvati's API.

## `parvati-poller`

This command sits running forever (optionally can be told to only run once) and pulls known games
from Parvati's hostlist API. For each game it finds, it will attempt to check the liveness of the
game and then update via the API to say what the status of the game is now.

### Building

`go build ./cmd/parvati-poller`

This will generate the given executable in the local directoty. Use `--help` for information on the
various settings.


## `soku-check-restd`

This is a little web-app that will take http REST requests and check soku games for liveness based on
the addresses (IPv4 and port) given to it.

Note that the cards CSV file it takes when running can be found in

https://github.com/misatosangel/soku-cardinfo/tree/master/data

and can be downloaded directly via (e.g,) `curl https://raw.githubusercontent.com/misatosangel/soku-cardinfo/master/data/all_cards.csv`

It exposes two interfaces detailed below:

#### `/ping/<address>`

This endpoint will simply check whether the given address looks like it has a live
hisoutensoku host behind it.

Example output (and yes, that IP is impossible)
```json
{
	"hostport": "398.266.314.244:10800",
	"request": "398.266.314.244:10800",
	"timeNS": 144252831,
	"up": true
}
```
The `request` mirrors whatever was actually asked for. This is normally the same as the actual `hostpost` found, but might not be ig given e.g. a hostname to and port rather than raw ip.

`timeNS` is the time the check took in nanoseconds.
`up` is the boolean indicating that the host is actually up.

#### `/check/<address>`

This endpoint is used to do a more detailed check on a host.

Example output for default level check (`level=basic`)
```json
{
	"hostport": "398.266.314.244:10800",
	"request": "398.266.314.244:10800",
	"result": {
		"address": "398.266.314.244:10800",
		"status": "Playing",
		"version": "1.10a",
		"spectate": 117,
		"additional": {
			"sokuroll": "1.3"
		}
	}
}
```

The `spectate` is actually a tristate boolean as a unicode character:

`u` - Specatte is unknown
`y` - Spectate is enabled
`n` - Spectate is disabled

Currently the endpoint only supports checking for soku-roll installed at v1.3 vs non-installed, and only supports checking histoutensoku v1.10a with full character linkage.

More information can be obtained by passing query paramter `level=state` or `level=full`.

##### `level=state`

This provides additional fields in the `result` object for a running game:
```json
{
		"profiles": ["profile1p", "profile2p"],
		"spec_chain": ["a.b.c.d:14728"],
}
```
Both may be missing if there's a problem following the spec chain. `profiles` are the profile names used by player 1 and 2 respectively. The `spec_chain` entry is an Array of IP and ports of spectators jumped through to get to the information. Due to the way soku spectating works (a 4-node tree of specators) then the entire spec tree is not shown here. Only the route the api query was sent through in order to get the game information.

##### `level=full`

This provides additional field in the `result` object for a running game:
```
{
    "game": {
        "players": [{
            "character": 4,
            "profile_num": 0,
            "deck_num": 1,
            "deck": [106, 106, 106, 106, 108, 112, 112, 112, 112, 201, 201, 201, 202, 202, 203, 203, 204, 205, 207, 207]
        }, {
            "character": 0,
            "profile_num": 0,
            "deck_num": 1,
            "deck": [13, 101, 101, 101, 101, 104, 104, 104, 104, 201, 201, 206, 206, 208, 208, 210, 210, 214, 214, 219]
        }],
        "music_track": 21,
        "rng_seed": "FJti",
        "count": 6
    },
}
```

### Building

`go build ./cmd/soku-check-restd`

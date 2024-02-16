module github.com/dextryz/ixian

go 1.22.0

replace github.com/dextryz/nostr => ../nostr

replace github.com/dextryz/pipe => ../pipe

replace github.com/dextryz/nip23 => ../nip23

replace github.com/dextryz/nip84 => ../nip84

require (
	github.com/dextryz/nip23 v0.0.0-00010101000000-000000000000
	github.com/dextryz/nip84 v0.0.0-00010101000000-000000000000
	github.com/dextryz/nostr v0.0.0-00010101000000-000000000000
	github.com/fiatjaf/eventstore v0.3.11
	github.com/gomarkdown/markdown v0.0.0-20231222211730-1d6d20845b47
	github.com/nbd-wtf/go-nostr v0.28.6
)

require (
	github.com/btcsuite/btcd/btcec/v2 v2.3.2 // indirect
	github.com/btcsuite/btcd/btcutil v1.1.3 // indirect
	github.com/btcsuite/btcd/chaincfg/chainhash v1.0.2 // indirect
	github.com/decred/dcrd/crypto/blake256 v1.0.1 // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.2.0 // indirect
	github.com/gobwas/httphead v0.1.0 // indirect
	github.com/gobwas/pool v0.2.1 // indirect
	github.com/gobwas/ws v1.3.1 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/puzpuzpuz/xsync/v3 v3.0.2 // indirect
	github.com/tidwall/gjson v1.17.0 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	golang.org/x/exp v0.0.0-20231006140011-7918f672742d // indirect
	golang.org/x/sys v0.14.0 // indirect
)

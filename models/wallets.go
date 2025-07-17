package models

import (
	"fmt"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/tyler-smith/go-bip32"
	bip39 "github.com/tyler-smith/go-bip39"
)

// --- Ethereum/BSC (ERC20/BEP20) ---
func GenerateEthWallet() (mnemonic, privKeyHex, address string, err error) {
	// Generate mnemonic
	entropy, err := bip39.NewEntropy(128)
	if err != nil {
		return
	}
	mnemonic, err = bip39.NewMnemonic(entropy)
	if err != nil {
		return
	}
	// Derive seed and master key
	seed := bip39.NewSeed(mnemonic, "")
	masterKey, err := bip32.NewMasterKey(seed)
	if err != nil {
		return
	}
	// Standard derivation path: m/44'/60'/0'/0/0
	purpose, _ := masterKey.NewChildKey(bip32.FirstHardenedChild + 44)
	coinType, _ := purpose.NewChildKey(bip32.FirstHardenedChild + 60)
	account, _ := coinType.NewChildKey(bip32.FirstHardenedChild + 0)
	change, _ := account.NewChildKey(0)
	addressKey, _ := change.NewChildKey(0)
	privKey, _ := crypto.ToECDSA(addressKey.Key)
	privKeyHex = fmt.Sprintf("%x", crypto.FromECDSA(privKey))
	address = crypto.PubkeyToAddress(privKey.PublicKey).Hex()
	return
}

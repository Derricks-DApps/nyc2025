package main

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

type artifact struct {
	ABI      json.RawMessage `json:"abi"`
	Bytecode struct {
		Object string `json:"object"`
	} `json:"bytecode"`
}

func mustGetEnv(k string) string {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		log.Fatalf("%s is not set", k)
	}
	return v
}

func main() {
	ctx := context.Background()

	// 1) Connect to Anvil
	rpc := "http://127.0.0.1:8545"
	client, err := ethclient.DialContext(ctx, rpc)
	if err != nil {
		log.Fatalf("dial: %v", err)
	}
	defer client.Close()

	// 2) Load private key
	rawKey := mustGetEnv("PRIVATE_KEY")
	rawKey = strings.TrimPrefix(rawKey, "0x")
	privKey, err := crypto.HexToECDSA(rawKey)
	if err != nil {
		log.Fatalf("private key parse: %v", err)
	}

	// 3) Chain ID (Anvil default 31337)
	chainID, err := client.ChainID(ctx)
	if err != nil {
		log.Fatalf("chain id: %v", err)
	}
	fmt.Println("Connected. ChainID:", chainID)

	// 4) Transact opts
	auth, err := bind.NewKeyedTransactorWithChainID(privKey, chainID)
	if err != nil {
		log.Fatalf("transactor: %v", err)
	}
	// Let bind auto-estimate gas; set a reasonable context deadline per tx
	gp, err := client.SuggestGasPrice(ctx)
	if err != nil {
		log.Fatalf("gas price: %v", err)
	}
	auth.GasPrice = gp

	// 5) Read Foundry artifact for ABI & bytecode
	artifactPath := filepath.Join("out", "HelloWorld.sol", "HelloWorld.json")
	raw, err := os.ReadFile(artifactPath)
	if err != nil {
		log.Fatalf("read artifact: %v", err)
	}

	var art artifact
	if err := json.Unmarshal(raw, &art); err != nil {
		log.Fatalf("unmarshal artifact: %v", err)
	}

	parsedABI, err := abi.JSON(strings.NewReader(string(art.ABI)))
	if err != nil {
		log.Fatalf("parse abi: %v", err)
	}

	bytecodeHex := strings.TrimPrefix(art.Bytecode.Object, "0x")
	bytecode, err := hex.DecodeString(bytecodeHex)
	if err != nil {
		log.Fatalf("decode bytecode: %v", err)
	}

	// 6) Deploy the contract with constructor arg
	auth.Context = ctxWithTimeout(ctx, 60*time.Second)
	address, tx, _, err := bind.DeployContract(auth, parsedABI, bytecode, client, "Hello from Go+Anvil!")
	if err != nil {
		log.Fatalf("deploy: %v", err)
	}
	fmt.Println("Deploy tx:", tx.Hash().Hex())
	fmt.Println("Contract address (pending):", address.Hex())

	// 7) Wait until mined
	rcpt, err := bind.WaitMined(ctx, client, tx)
	if err != nil {
		log.Fatalf("wait mined: %v", err)
	}
	if rcpt.Status != 1 {
		log.Fatalf("deployment failed: status %d", rcpt.Status)
	}
	fmt.Println("Contract deployed at:", address.Hex())

	// 8) Call greet()
	bound := bind.NewBoundContract(address, parsedABI, client, client, client)
	var greeting string
	if err := bound.Call(&bind.CallOpts{Context: ctx}, &greeting, "greet"); err != nil {
		log.Fatalf("call greet: %v", err)
	}
	fmt.Println("greet():", greeting)

	// 9) Update greeting via transaction
	auth.Context = ctxWithTimeout(ctx, 60*time.Second)
	tx2, err := bound.Transact(auth, "setGreeting", "Updated from Go!")
	if err != nil {
		log.Fatalf("setGreeting tx: %v", err)
	}
	fmt.Println("setGreeting tx:", tx2.Hash().Hex())
	if _, err := bind.WaitMined(ctx, client, tx2); err != nil {
		log.Fatalf("wait mined 2: %v", err)
	}

	// 10) Call greet() again
	if err := bound.Call(&bind.CallOpts{Context: ctx}, &greeting, "greet"); err != nil {
		log.Fatalf("call greet 2: %v", err)
	}
	fmt.Println("greet() after update:", greeting)

	// 11) Print sender for reference
	pub := privKey.Public().(*ecdsa.PublicKey)
	from := crypto.PubkeyToAddress(*pub)
	bal, _ := client.BalanceAt(ctx, from, nil)
	fmt.Printf("Deployer: %s  Balance: %s wei\n", from.Hex(), bal.String())
}

func ctxWithTimeout(parent context.Context, d time.Duration) context.Context {
	c, _ := context.WithTimeout(parent, d)
	return c
}

package main

import (
	"log"
	"fmt"
	"encoding/hex"
	"crypto/ecdsa"
	"github.com/shopspring/decimal"

	"github.com/samott/crash-backend/config"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
	"github.com/ethereum/go-ethereum/common/math"
)

var types = apitypes.Types{
	"EIP712Domain": {
		{ Name: "name", Type: "string" },
		{ Name: "version", Type: "string" },
		{ Name: "chainId", Type: "uint256" },
		{ Name: "verifyingContract", Type: "address" },
	},
	"WithdrawalRequest": {
		{ Name: "user", Type: "address" },
		{ Name: "coinId", Type: "uint32" },
		{ Name: "amount", Type: "uint256" },
		{ Name: "nonce", Type: "uint256" },
		{ Name: "tasks", Type: "Task[]" },
	},
	"Task": {
		{ Name: "taskType", Type: "uint8" },
		{ Name: "user", Type: "address" },
		{ Name: "coinId", Type: "uint32" },
		{ Name: "amount", Type: "uint256" },
		{ Name: "nonce", Type: "uint256" },
	},
}

type Task struct {
	taskType int8;
	user string;
	coinId string;
	amount string;
	nonce string;
}

type WithdrawalRequest struct {
	user string;
	coinid string;
	amount string;
	nonce string;
	tasks []Task;
}

func createWithdrawalRequest(
	wallet string,
	amount decimal.Decimal,
	currency string,
	chainId int64,
	nonce int64,
	cfg *config.CrashConfig,
) (string, error) {
	domain := apitypes.TypedDataDomain{
		Name:              "Crash",
		Version:           "1.0",
		ChainId:           math.NewHexOrDecimal256(chainId),
		VerifyingContract: cfg.OnChain.Contract,
	};

	coinId := decimal.NewFromInt(int64(cfg.Currencies[currency].CoinId));
	decNonce := decimal.NewFromInt(int64(nonce));
	decimals :=  int64(cfg.Currencies[currency].Decimals);

	scale := decimal.NewFromInt(10).Pow(decimal.NewFromInt(decimals));

	message := apitypes.TypedDataMessage{
		"user":   wallet,
		"coinId": coinId.String(),
		"amount": amount.Mul(scale).String(),
		"nonce":  decNonce.String(),
		"tasks":  []map[string]any{},
	};

	typedData := apitypes.TypedData{
		Types:       types,
		PrimaryType: "WithdrawalRequest",
		Domain:      domain,
		Message:     message,
	}

	// addr = 0x5630f29Bd82793801446b3deF50B0Fd50F972252
	privateKey, err := crypto.HexToECDSA("cbfc67bba0255709891f5feebc584462aa2966bbf60d2e000d6ff215cfe48610");

	if err != nil {
		log.Fatal("Error loading private key: ", err);
	}

	sig, err := signTypedData(typedData, privateKey);

	if err != nil {
		return "", err;
	}

	sigStr := "0x" + hex.EncodeToString(sig);

	return sigStr, nil;
}

func signTypedData(data apitypes.TypedData, privateKey *ecdsa.PrivateKey) ([]byte, error) {
	domainSeparator, err := data.HashStruct("EIP712Domain", data.Domain.Map());

	if err != nil {
		return nil, err;
	}

	typedDataHash, err := data.HashStruct(data.PrimaryType, data.Message);

	if err != nil {
		return nil, err;
	}

	rawData := []byte(fmt.Sprintf("\x19\x01%s%s", string(domainSeparator), string(typedDataHash)));

	sighash := crypto.Keccak256(rawData);

	signature, err := crypto.Sign(sighash, privateKey);

	if err != nil {
		return nil, err;
	}

	if signature[64] == 0 || signature[64] == 1 {
		signature[64] += 27;
	}

	return signature, nil;
}

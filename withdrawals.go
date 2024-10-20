package main

import (
	"crypto/ecdsa"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"os"

	"github.com/shopspring/decimal"

	"github.com/samott/crash-backend/config"

	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
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

var privateKey = "";

type Task struct {
	taskType int8;
	user string;
	coinId string;
	amount string;
	nonce string;
}

type WithdrawalRequest struct {
	user string;
	coinId string;
	amount string;
	nonce string;
	tasks []Task;
}

func init() {
	privateKey = os.Getenv("AGENT_PRIVATE_KEY");

	if privateKey == "" {
		log.Fatal("AGENT_PRIVATE_KEY not defined")
	}

	if strings.HasPrefix(privateKey, "0x") {
		privateKey = privateKey[2:];
	}

	log.Fatal(privateKey);
}

func createWithdrawalRequest(
	wallet string,
	amount decimal.Decimal,
	currency string,
	chainId int64,
	nonce int64,
	cfg *config.CrashConfig,
) (*WithdrawalRequest, string, error) {
	domain := apitypes.TypedDataDomain{
		Name:              "Crash",
		Version:           "1.0",
		ChainId:           math.NewHexOrDecimal256(chainId),
		VerifyingContract: cfg.OnChain.Contract,
	};

	coinId := decimal.NewFromInt(int64(cfg.Currencies[currency].CoinId));
	decNonce := decimal.NewFromInt(int64(nonce));
	decimals := int64(cfg.Currencies[currency].Decimals);

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

	privateKey, err := crypto.HexToECDSA(privateKey);

	if err != nil {
		log.Fatal("Error loading private key: ", err);
	}

	sig, err := signTypedData(typedData, privateKey);

	if err != nil {
		return nil, "", err;
	}

	sigStr := "0x" + hex.EncodeToString(sig);

	req := WithdrawalRequest{
		user: wallet,
		coinId: coinId.String(),
		amount: amount.String(),
		nonce: decNonce.String(),
		tasks: []Task{},
	};

	return &req, sigStr, nil;
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

func saveWithdrawalRequest(
	req *WithdrawalRequest,
	sig string,
	tx *sql.Tx,
) (error) {
	reqStr, err := json.Marshal(req);

	if err != nil {
		return err;
	}

	_, err = tx.Exec(`
		INSERT INTO withdrawals
		(wallet, nonce, amount, currency, signature, request)
		VALUES
		(?, ?, ?, ?, ?, ?)
	`, req.user, req.nonce, req.amount, sig, reqStr);

	return err;
}

func getNextNonce(
	db *sql.DB,
	wallet string,
) (int64, error) {
	var nonce int64;

	rows, err := db.Query(`
		SELECT MAX(nonce)
		FROM withdrawals
		WHERE wallet = ?
		GROUP BY wallet
		LIMIT 1
	`, wallet);

	if err != nil {
		return 0, err;
	}

	defer rows.Close();

	if !rows.Next() {
		return 0, nil;
	}

	rows.Scan(&nonce);

	return nonce, nil;
}

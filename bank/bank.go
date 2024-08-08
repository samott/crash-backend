package bank;

import (
	"database/sql"
	"errors"
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

var (
	ErrUnableToWithdrawBalance = errors.New("Unable to withdraw balance")
	ErrUnableToDecreaseBalance = errors.New("Unable to decrease balance")
	ErrUnableToIncreaseBalance = errors.New("Unable to increase balance")
	ErrBalanceRecordNotFound = errors.New("Balance record not found");
)

type TxCallback func(*sql.Tx) error;

type Bank struct {
	db *sql.DB;
};

func NewBank(db *sql.DB) (*Bank, error) {
	return &Bank{
		db: db,
	}, nil;
}

func (bank *Bank) DecreaseBalance(
	wallet string,
	currency string,
	amount decimal.Decimal,
	reason string,
	gameId uuid.UUID,
) (decimal.Decimal, error) {
	amountStr := amount.String();
	amountNegStr := amount.Neg().String();

	tx, err := bank.db.BeginTx(context.Background(), nil);

	if err != nil {
		return decimal.Zero, err;
	}

	defer tx.Rollback();

	result, err := tx.Exec(`
		UPDATE balances
		SET spent = spent + CAST(? AS Decimal(32, 18))
		WHERE wallet = ?
		AND currency = ?
		AND (balance - spent - withdrawn - ?) >= 0
	`, amountStr, wallet, currency, amountStr);

	if err != nil {
		return decimal.Zero, err;
	}

	if rows, err := result.RowsAffected(); rows == 0 || err != nil {
		return decimal.Zero, ErrUnableToDecreaseBalance;
	}

	result, err = tx.Exec(`
		INSERT INTO ledger
		(wallet, currency, change, reason, gameId)
		VALUES
		(?, ?, CAST(? AS Decimal(32, 18)), ?, ?)
	`, wallet, currency, amountNegStr, reason, gameId.String());

	if err := tx.Commit(); err != nil {
		return decimal.Zero, nil;
	}

	return bank.GetBalance(wallet, currency);
}

func (bank *Bank) IncreaseBalance(
	wallet string,
	currency string,
	amount decimal.Decimal,
	reason string,
	gameId uuid.UUID,
) (decimal.Decimal, error) {
	amountStr := amount.String();

	tx, err := bank.db.BeginTx(context.Background(), nil);

	if err != nil {
		return decimal.Zero, err;
	}

	defer tx.Rollback();

	result, err := tx.Exec(`
		UPDATE balances
		SET gained = gained + CAST(? AS Decimal(32, 18))
		WHERE wallet = ?
		AND currency = ?
	`, amountStr, wallet, currency);

	if err != nil {
		return decimal.Zero, err;
	}

	if rows, err := result.RowsAffected(); rows == 0 || err != nil {
		return decimal.Zero, ErrUnableToIncreaseBalance;
	}

	result, err = tx.Exec(`
		INSERT INTO ledger
		(wallet, currency, change, reason, gameId)
		VALUES
		(?, ?, CAST(? AS Decimal(32, 18)), ?, ?)
	`, wallet, currency, amountStr, reason, gameId.String());

	if err := tx.Commit(); err != nil {
		return decimal.Zero, nil;
	}

	return bank.GetBalance(wallet, currency);
}

func (bank *Bank) WithdrawBalance(
	wallet string,
	currency string,
	amount decimal.Decimal,
	txCallback TxCallback,
) (decimal.Decimal, error) {
	amountStr := amount.String();
	amountNegStr := amount.Neg().String();

	tx, err := bank.db.BeginTx(context.Background(), nil);

	if err != nil {
		return decimal.Zero, err;
	}

	defer tx.Rollback();

	result, err := tx.Exec(`
		UPDATE balances
		SET withdrawn = withdrawn + CAST(? AS Decimal(32, 18))
		WHERE wallet = ?
		AND currency = ?
		AND (balance - spent - withdrawn - ?) >= 0
	`, amountStr, wallet, currency, amountStr);

	if err != nil {
		return decimal.Zero, err;
	}

	if rows, err := result.RowsAffected(); rows == 0 || err != nil {
		return decimal.Zero, ErrUnableToWithdrawBalance;
	}

	result, err = tx.Exec(`
		INSERT INTO ledger
		(wallet, currency, change, reason, gameId)
		VALUES
		(?, ?, CAST(? AS Decimal(32, 18)), ?, NULL)
	`, wallet, currency, amountNegStr, "Withdrawal");

	if (txCallback != nil) {
		err = txCallback(tx);

		if err != nil {
			return decimal.Zero, ErrUnableToWithdrawBalance;
		}
	}

	if err := tx.Commit(); err != nil {
		return decimal.Zero, nil;
	}

	return bank.GetBalance(wallet, currency);
}

func (bank *Bank) GetBalance(
	wallet string,
	currency string,
) (decimal.Decimal, error) {
	var balanceStr string;

	rows, err := bank.db.Query(`
		SELECT balance + gained - spent - withdrawn AS balance
		FROM balances
		WHERE wallet = ?
		AND currency = ?
		LIMIT 1
	`, wallet, currency);

	if err != nil {
		return decimal.Zero, err;
	}

	defer rows.Close();

	if !rows.Next() {
		return decimal.Zero, ErrBalanceRecordNotFound;
	}

	rows.Scan(&balanceStr);

	balance, err := decimal.NewFromString(balanceStr);

	if err != nil {
		return decimal.Zero, err;
	}

	return balance, nil;
}

func (bank *Bank) GetBalances(
	wallet string,
) (map[string]decimal.Decimal, error) {
	balances := make(map[string]decimal.Decimal);

	rows, err := bank.db.Query(`
		SELECT currency, balance + gained - spent - withdrawn AS balance
		FROM balances
		WHERE wallet = ?
	`, wallet);

	if err != nil {
		return balances, err;
	}

	defer rows.Close();

	for rows.Next() {
		var (
			currency string
			balanceStr string
		);

		rows.Scan(&currency, &balanceStr);

		balance, err := decimal.NewFromString(balanceStr);

		if err != nil {
			slog.Error(
				"Unable to load decimal balance from database",
				"wallet",
				wallet,
				"currency",
				currency,
			);

			balance = decimal.Zero;
		}

		balances[currency] = balance;
	}


	return balances, nil;
}

package rates;

import (
	"testing"

	"github.com/h2non/gock"
	"github.com/shopspring/decimal"
)

func TestFetchRates(t *testing.T) {
	defer gock.Off();

	gock.New("https://api.coingecko.com").
		Get("/api/v3/simple/price").
		Reply(200).
		BodyString(`{"bitcoin":{"usd":60000,"eur":50000},"ethereum":{"usd":3000,"eur":2000}}`);

	config := RatesConfig{
		ApiKey: "",
		Cryptos: map[string]string{
			"btc": "bitcoin",
			"eth": "ethereum",
		},
		Fiats: []string{"usd", "eur"},
	};

	ratesSvc := NewService(&config);

	result, err := ratesSvc.FetchRates();

	if err != nil {
		t.Fatal("Rates returned error: ", err);
	}

	if _, ok := result["btc"]; !ok { t.Fatal("Missing btc in result"); }
	if _, ok := result["eth"]; !ok { t.Fatal("Missing eth in result"); }
	if _, ok := result["btc"]["usd"]; !ok { t.Fatal("Missing btc:usd in result"); }
	if _, ok := result["btc"]["eur"]; !ok { t.Fatal("Missing btc:eur in result"); }
	if _, ok := result["eth"]["usd"]; !ok { t.Fatal("Missing eth:usd in result"); }
	if _, ok := result["eth"]["eur"]; !ok { t.Fatal("Missing eth:eur in result"); }

	if v, _ := decimal.NewFromString("60000"); !v.Equal(result["btc"]["usd"]) {
		t.Fatal("btc:usd value not correct:", v, "vs.", result["btc"]["usd"]);
	}

	if v, _ := decimal.NewFromString("50000"); !v.Equal(result["btc"]["eur"]) {
		t.Fatal("btc:eur value not correct:", v, "vs.", result["btc"]["eur"]);
	}

	if v, _ := decimal.NewFromString("3000"); !v.Equal(result["eth"]["usd"]) {
		t.Fatal("eth:usd value not correct:", v, "vs.", result["eth"]["usd"]);
	}

	if v, _ := decimal.NewFromString("2000"); !v.Equal(result["eth"]["eur"]) {
		t.Fatal("eth:eur value not correct:", v, "vs.", result["eth"]["eur"]);
	}
}

func TestFetchRatesWithMissingCurrency(t *testing.T) {
	defer gock.Off();

	gock.New("https://api.coingecko.com").
		Get("/api/v3/simple/price").
		Reply(200).
		BodyString(`{"bitcoin":{"usd":60000,"eur":50000}`);

	config := RatesConfig{
		ApiKey: "",
		Cryptos: map[string]string{
			"btc": "bitcoin",
			"eth": "ethereum",
		},
		Fiats: []string{"usd", "eur"},
	};

	ratesSvc := NewService(&config);

	_, err := ratesSvc.FetchRates();

	if err == nil {
		t.Fatal("Rates improperly accepted result with missing currency: ", err);
	}
}

func TestFetchRatesWithMissingFiat(t *testing.T) {
	defer gock.Off();

	gock.New("https://api.coingecko.com").
		Get("/api/v3/simple/price").
		Reply(200).
		BodyString(`{"bitcoin":{"usd":60000}`);

	config := RatesConfig{
		ApiKey: "",
		Cryptos: map[string]string{
			"btc": "bitcoin",
		},
		Fiats: []string{"usd", "eur"},
	};

	ratesSvc := NewService(&config);

	_, err := ratesSvc.FetchRates();

	if err == nil {
		t.Fatal("Rates improperly accepted result with missing fiat currency: ", err);
	}
}

func TestFetchRatesWithInvalid(t *testing.T) {
	defer gock.Off();

	gock.New("https://api.coingecko.com").
		Get("/api/v3/simple/price").
		Reply(200).
		BodyString(`{"bitcoin":{"usd":"wrong","eur":20000}`);

	config := RatesConfig{
		ApiKey: "",
		Cryptos: map[string]string{
			"btc": "bitcoin",
			"eth": "ethereum",
		},
		Fiats: []string{"usd", "eur"},
	};

	ratesSvc := NewService(&config);

	_, err := ratesSvc.FetchRates();

	if err == nil {
		t.Fatal("Rates improperly parsed invalid response: ", err);
	}
}

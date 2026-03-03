package main

import (
	"fmt"
	"math/rand"
	"time"
)

// CasinoGame — тип игры
type CasinoGame string

const (
	GameSlots    CasinoGame = "slots"
	GameCoinflip CasinoGame = "coinflip"
	GameDice     CasinoGame = "dice"
)

// CasinoResult — результат игры
type CasinoResult struct {
	Win        bool
	Multiplier float64
	Payout     float64 // итоговая сумма к выплате (bet * Multiplier)
	Display    string
	Message    string
}

// HandleCasino — вызывается из main.go в switch d.Action case "casino"
// Пример использования:
//
//	case "casino":
//	    newBal, msg := HandleCasino(uid, d.Game, d.Bet, d.Win, d.Payout, getBalance, setBalance)
//	    _ = newBal
//	    if msg != "" { return c.Send(msg) }
//	    return nil
func HandleCasino(uid, game string, bet float64, win bool, payout float64,
	getBalance func(string) float64,
	setBalance func(string, float64),
) (newBalance float64, errMsg string) {
	cur := getBalance(uid)
	if cur < bet {
		return cur, "❌ Недостаточно средств"
	}
	if win {
		newBalance = cur - bet + payout
	} else {
		newBalance = cur - bet
	}
	setBalance(uid, newBalance)
	return newBalance, ""
}

var rng = rand.New(rand.NewSource(time.Now().UnixNano()))

// ─────────────────────────────────────────
//  СЛОТЫ  (шанс выигрыша ~20%)
// ─────────────────────────────────────────

var slotSymbols = []string{"🍒", "🍋", "🍊", "🍇", "⭐", "💎", "7️⃣"}

// Веса символов (чем меньше индекс — тем чаще)
var slotWeights = []int{30, 25, 20, 15, 6, 3, 1} // сумма = 100

// Множители за три одинаковых
var slotMultipliers = map[string]float64{
	"🍒": 1.5,
	"🍋": 1.8,
	"🍊": 2.0,
	"🍇": 2.5,
	"⭐": 4.0,
	"💎": 8.0,
	"7️⃣": 15.0,
}

func weightedSymbol() string {
	total := 0
	for _, w := range slotWeights {
		total += w
	}
	n := rng.Intn(total)
	for i, w := range slotWeights {
		n -= w
		if n < 0 {
			return slotSymbols[i]
		}
	}
	return slotSymbols[0]
}

func PlaySlots(bet float64) CasinoResult {
	s1 := weightedSymbol()
	s2 := weightedSymbol()
	s3 := weightedSymbol()
	display := fmt.Sprintf("[ %s | %s | %s ]", s1, s2, s3)

	// Три одинаковых — выигрыш
	if s1 == s2 && s2 == s3 {
		mult := slotMultipliers[s1]
		return CasinoResult{
			Win:        true,
			Multiplier: mult,
			Payout:     bet * mult,
			Display:    display,
			Message:    fmt.Sprintf("🎰 %s\n🎉 ДЖЕКПОТ! Три %s!\n💰 Выигрыш: %.2f GOLD (x%.1f)", display, s1, bet*mult, mult),
		}
	}

	// Два одинаковых — возврат половины
	if s1 == s2 || s2 == s3 || s1 == s3 {
		return CasinoResult{
			Win:        true,
			Multiplier: 0.5,
			Payout:     bet * 0.5,
			Display:    display,
			Message:    fmt.Sprintf("🎰 %s\n😅 Два одинаковых! Возврат половины: %.2f GOLD", display, bet*0.5),
		}
	}

	return CasinoResult{
		Win:        false,
		Multiplier: 0,
		Display:    display,
		Message:    fmt.Sprintf("🎰 %s\n😔 Не повезло! Потеряно %.2f GOLD", display, bet),
	}
}

// ─────────────────────────────────────────
//  МОНЕТКА  (шанс выигрыша ~47%)
// ─────────────────────────────────────────

func PlayCoinflip(bet float64, choice string) CasinoResult {
	// Чуть меньше 50% в пользу казино
	win := rng.Intn(100) < 47
	var result string
	if win {
		result = choice
	} else {
		if choice == "орёл" {
			result = "решка"
		} else {
			result = "орёл"
		}
	}

	coin := "🪙 ОРЁЛ"
	if result == "решка" {
		coin = "🪙 РЕШКА"
	}

	if win {
		return CasinoResult{
			Win:        true,
			Multiplier: 1.9,
			Payout:     bet * 1.9,
			Display:    coin,
			Message:    fmt.Sprintf("%s\n✅ Вы угадали! Выигрыш: %.2f GOLD", coin, bet*1.9),
		}
	}

	return CasinoResult{
		Win:        false,
		Multiplier: 0,
		Display:    coin,
		Message:    fmt.Sprintf("%s\n❌ Не угадали! Потеряно %.2f GOLD", coin, bet),
	}
}

// ─────────────────────────────────────────
//  КОСТИ  (угадай число 1–6, шанс ~16%)
// ─────────────────────────────────────────

func PlayDice(bet float64, guess int) CasinoResult {
	if guess < 1 || guess > 6 {
		return CasinoResult{Message: "❌ Выберите число от 1 до 6"}
	}

	rolled := rng.Intn(6) + 1
	diceEmoji := []string{"", "1️⃣", "2️⃣", "3️⃣", "4️⃣", "5️⃣", "6️⃣"}
	display := fmt.Sprintf("🎲 Выпало: %s", diceEmoji[rolled])

	if rolled == guess {
		return CasinoResult{
			Win:        true,
			Multiplier: 5.0,
			Payout:     bet * 5.0,
			Display:    display,
			Message:    fmt.Sprintf("%s\n🎯 Угадали! Выигрыш: %.2f GOLD (x5)", display, bet*5.0),
		}
	}

	return CasinoResult{
		Win:        false,
		Multiplier: 0,
		Display:    display,
		Message:    fmt.Sprintf("%s\n😔 Вы поставили на %s. Потеряно %.2f GOLD", display, diceEmoji[guess], bet),
	}
}

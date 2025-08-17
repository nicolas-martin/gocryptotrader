1. Trend Filter: Moving Averages

Indicator: 50-period EMA (short-term) and 200-period EMA (long-term).

Entry Trigger: Only consider longs if 50EMA > 200EMA, shorts if 50EMA < 200EMA.

Exit Trigger: Close if crossover happens against your position.

Use Case: Keeps you trading with the broader trend instead of against it.

2. Momentum Confirmation: RSI

Indicator: RSI (14).

Entry Triggers:

Long: RSI crosses above 40 (from oversold or mid-level).

Short: RSI crosses below 60 (from overbought or mid-level).

Exit Triggers:

Long: RSI > 70 then starts turning down.

Short: RSI < 30 then starts turning up.

Use Case: Avoids chasing extended moves, only enters when momentum is turning in your favor.

3. Volatility Check: Bollinger Bands

Indicator: 20-period SMA with 2 std dev bands.

Entry Triggers:

Long: Price closes above middle band after touching lower band (volatility squeeze recovery).

Short: Price closes below middle band after touching upper band.

Exit Triggers:

Long: Price closes below middle band again.

Short: Price closes above middle band again.

Use Case: Prevents entries during low-vol chop, looks for breakouts or reversals with volatility.

4. Volume Confirmation: OBV or VWAP

Indicator: On-Balance Volume (OBV) or VWAP (session-based).

Entry Triggers:

Long: OBV trending up while price confirms other signals.

Short: OBV trending down.

Exit Trigger: Divergence (price up but OBV flat/down = exit longs).

Use Case: Confirms that moves aren’t just “thin air” but backed by actual flow.

Putting It Together – Entry/Exit Criteria

Good Long Entry:

50EMA > 200EMA (trend up).

RSI crosses above 40.

Price recovers above middle Bollinger band after touching the lower band.

OBV trending up.
→ Enter long.

Good Short Entry:

50EMA < 200EMA (trend down).

RSI crosses below 60.

Price falls below middle Bollinger band after touching the upper band.

OBV trending down.
→ Enter short.

Exits:

Stop-loss just outside last swing low/high.

Exit if RSI flips against position (e.g., long and RSI rolls down from 70).

Exit if OBV diverges.

Optional: fixed TP at 1.5–2× risk.

Example Parameter Settings

EMA periods: 50 & 200.

RSI levels: 40 (long trigger), 60 (short trigger), 70/30 (exit zones).

Bollinger: 20 SMA, 2 std dev.

OBV: cumulative, smoothed with EMA(10).

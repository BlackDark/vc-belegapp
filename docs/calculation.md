# Calculation

> **Keine Steuerberatung.** This is not tax advice. The app documents Belege and prepares figures. The employer's payroll (Lohnabrechnung) is what counts. The PDF and the UI say so.

Amounts are integer cents. Rates are basis points (1 bp = 0.01%). The Sachbezugswert (SBW) is the official value of one meal for that year (2026: breakfast 2,37 €, lunch and dinner 4,57 €). The Höchstzuschuss is that SBW plus 3,10 €. A Zuschuss above the Höchstzuschuss is treated as regular taxable wages.

Per Beleg, with meal value `A` (corrected amount if set, otherwise the receipt total), daily subsidy `Z`, and SBW `S`:

```text
E = min(Z, A)                         Erstattung (amount paid back)
U = A - E                             Eigenanteil (employee share)
H = S + 3,10 €                        Höchstzuschuss
if the meal type is not subsidised:   E = U = G = F = R = 0
if Z > H:                             G = 0, F = 0, R = E
else, variant standard:               G = max(0, min(E, S - U)), F = E - G, R = 0
else, variant vorsichtig:             G = min(E, S), F = E - G, R = 0
```

`G` is the geldwerter Vorteil (taxable benefit), `F` the tax-free part, `R` the part taxed as normal wages.

For the month, when Pauschalierung is on, Pauschalsteuer is 25% wage tax on the sum of `G`, plus Solidaritätszuschlag and Kirchensteuer, paid by the employer. Lohnsteuer is rounded half up; Soli and Kirchensteuer are rounded down. With the default rates (25%, Soli 5,5%, Kirchensteuer from the Bundesland):

```text
LSt  = (ΣG × pauschsteuersatz_bp + 5000) div 10000
Soli = (LSt × soli_satz_bp) div 10000
KiSt = (LSt × kist_satz_bp) div 10000
employer cost = ΣE + LSt + Soli + KiSt
```

With Pauschalierung off, those three taxes are 0 and `ΣG + ΣR` is left as individually taxable wages.

October 2026, lunch, North Rhine-Westphalia, subsidy 7,67 €, the five receipts in the [README](../README.md) screenshot grid: Erstattung 33,01 €, geldwerter Vorteil 16,78 €, Lohnsteuer 4,20 €, Soli 0,23 €, Kirchensteuer 0,29 €, employer cost 37,73 €. Vectors and the other variant live in [SPEC.md](SPEC.md) §6. Terms: [GLOSSARY.md](GLOSSARY.md). Background notes: [research/steuer.md](research/steuer.md).

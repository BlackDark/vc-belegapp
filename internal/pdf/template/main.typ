// Monthly Nachweis. Reads daten.json. No packages and no network.
#let data = json("daten.json")
#let entwurf = sys.inputs.at("entwurf", default: "false") == "true"
#let autor = if data.meta.autor == "" { "vc-belegapp" } else { data.meta.autor }

#set document(title: data.meta.pdf_titel, author: autor, keywords: data.meta.dokument_id)
#set text(font: "Inter", size: 10pt, fill: rgb("#1a1a1a"), lang: "de", hyphenate: false)

#let background = if entwurf {
  align(center + horizon, rotate(45deg, text(size: 64pt, fill: rgb("#c8c8c8"), weight: "semibold")[ENTWURF]))
} else { [] }

#let footer = context {
  let cur = counter(page).get().first()
  let tot = counter(page).final().first()
  let mark = if entwurf { " · ENTWURF" } else { "" }
  set text(size: 8pt, fill: rgb("#404040"))
  align(center)[#data.meta.dokument_id#mark · Seite #cur von #tot · vc-belegapp #data.meta.app_version]
}

#set page(
  paper: "a4",
  flipped: true,
  margin: (x: 10mm, y: 12mm),
  numbering: none,
  background: background,
  footer: footer,
)

#text(size: 14pt, weight: "semibold")[#data.meta.titel]
#v(1mm)
#align(right)[
  #text(size: 9pt)[Dokument #data.meta.dokument_id #h(4mm) #data.meta.erstellt]
]

#v(2mm)
#text(weight: "medium")[Stammdaten]
#v(1mm)
Arbeitnehmer: #data.stamm.arbeitnehmer #h(6mm)
Personalnummer: #data.stamm.personalnummer #h(6mm)
Arbeitgeber: #data.stamm.arbeitgeber

#v(2mm)
#text(weight: "semibold")[Jahresregel]
#v(1mm)
#for paar in data.regel [
  #text(fill: rgb("#52525b"))[#paar.label:] #paar.wert #h(4mm)
]
#if data.regel_hinweise.len() > 0 {
  v(1mm)
  for hinweis in data.regel_hinweise [
    #text(size: 9pt, fill: rgb("#3f3f46"))[#hinweis] \
  ]
}

#v(3mm)
#text(weight: "semibold")[Belege]
#v(1mm)

#let cols = (
  8mm, 16mm, 8mm, 18mm, 20mm, 16mm, 34mm,
  16mm, 16mm, 16mm, 16mm, 14mm, 16mm, 14mm,
  26mm, 12mm,
)

#let anhang-seite(nr) = context {
  let hits = query(label("anhang-" + nr))
  if hits.len() > 0 { str(hits.first().location().page()) } else { "–" }
}

#let beleg-cells(row) = (
  [#row.nr],
  [#row.datum],
  [#row.wt],
  [#row.mahlzeit],
  [#row.bezugsort],
  [#row.arbeitsort],
  [#row.haendler],
  [#row.beleg],
  [#row.anerkannt],
  [#row.erstattung],
  [#row.eigenanteil],
  [#row.gv],
  [#row.steuerfrei],
  [#row.regulaer],
  [#row.hinweise],
  if row.summe { [–] } else { anhang-seite(row.nr) },
)

#if data.belege.len() == 0 [
  Keine Belege in diesem Monat.
] else [
  #text(size: 9pt)[
    #show table.cell.where(y: 0): set text(fill: white, weight: "semibold", size: 8pt)
    #table(
      columns: cols,
      inset: 2pt,
      stroke: 0.4pt + rgb("#d4d4d8"),
      fill: (x, y) => if y == 0 { rgb("#3f3f46") } else if calc.odd(y) { rgb("#f4f4f5") } else { rgb("#ffffff") },
      align: (x, y) => if x >= 7 and x <= 13 { right } else { left },
      table.header(
        [Nr.], [Datum], [Wt], [Mahlzeit], [Bezugsort], [Arbeitsort], [Händler, Ort],
        [Beleg], [Anerkannt], [Erstattung], [Eigenanteil], [GV], [Steuerfrei], [Regulär],
        [Hinweise], [Anhang],
      ),
      ..data.belege.map(beleg-cells).flatten(),
    )
  ]
]

#if data.fussnoten.len() > 0 {
  v(1mm)
  set text(size: 8pt)
  for (i, note) in data.fussnoten.enumerate() [
    [#(i + 1)] #note \
  ]
}

#v(3mm)
#text(weight: "semibold")[Summen]
#v(1mm)
#let sum-line(label, wert) = [#text(fill: rgb("#52525b"))[#label:] #wert #h(4mm)]
#sum-line("Anzahl", data.summen.anzahl)
#sum-line("Σ Belegbetrag", data.summen.belegbetrag)
#sum-line("Σ Anerkannt", data.summen.anerkannt)
#sum-line("Σ Erstattung", data.summen.erstattung)
#sum-line("Σ Eigenanteil", data.summen.eigenanteil)
#sum-line("Σ Geldwerter Vorteil", data.summen.gv)
#sum-line("Σ Steuerfrei", data.summen.steuerfrei)
#sum-line("Σ Regulär", data.summen.regulaer)
#if data.summen.pauschalierung [
  #sum-line("Pauschalsteuer", data.summen.pauschalsteuer)
  #sum-line("Solidaritätszuschlag", data.summen.soli)
  #sum-line("Kirchensteuer", data.summen.kist)
  #sum-line("Pauschal gesamt", data.summen.pauschal_gesamt)
  #sum-line("Arbeitgeberkosten", data.summen.ag_kosten)
] else [
  #v(1mm)
  #data.summen.ohne_pauschal
  #h(4mm)
  #sum-line("AN-pflichtig", data.summen.an_pflichtig)
  #sum-line("Arbeitgeberkosten", data.summen.ag_kosten)
]

#v(3mm)
#text(weight: "semibold")[Prüfpunkte]
#v(1mm)
#for check in data.pruefpunkte [
  #check.symbol #check.text \
]

#if data.meta.version >= 2 [
  #v(3mm)
  #text(weight: "semibold")[Änderungen gegenüber Version #(data.meta.version - 1)]
  #v(1mm)
  #if data.aenderungen.len() == 0 [
    Keine Feldänderungen seit der vorigen Exportversion.
  ] else [
    #text(size: 8pt)[
      #table(
        columns: (32mm, 42mm, 28mm, 1fr, 1fr, 40mm),
        inset: 2pt,
        stroke: 0.4pt + rgb("#d4d4d8"),
        fill: (x, y) => if y == 0 { rgb("#e4e4e7") } else { rgb("#ffffff") },
        table.header([Zeitpunkt], [Beleg], [Feld], [Alt], [Neu], [Änderungsgrund]),
        ..data.aenderungen.map(row => (
          [#row.zeitpunkt], [#row.bezug], [#row.feld], [#row.alt], [#row.neu], [#row.grund],
        )).flatten(),
      )
    ]
  ]
]

#v(3mm)
#text(weight: "semibold")[Arbeitnehmererklärung]
#v(1mm)
#rect(width: 100%, fill: rgb("#f4f4f5"), inset: 8pt, stroke: 0.4pt + rgb("#d4d4d8"))[#data.erklaerung]
#v(1mm)
#text(size: 9pt)[#data.bestaetigung]
#v(2mm)
Ort, Datum, Unterschrift Arbeitnehmer: #box(width: 70mm, stroke: (bottom: 0.4pt + rgb("#1a1a1a")))[]
#h(6mm)
Geprüft (Arbeitgeber): Datum/Kürzel #box(width: 50mm, stroke: (bottom: 0.4pt + rgb("#1a1a1a")))[]
#v(2mm)
#text(size: 8pt, fill: rgb("#52525b"))[#data.hinweis]

#if data.anhang.len() > 0 {
  set page(
    paper: "a4",
    flipped: false,
    margin: 14mm,
    numbering: none,
    background: background,
    footer: footer,
  )
  for (i, seite) in data.anhang.enumerate() {
    if i > 0 { pagebreak() }
    if seite.erste {
      [#metadata(seite.nr)#label("anhang-" + seite.nr)]
    }
    text(weight: "semibold")[Nr. #seite.nr · #seite.datum · #seite.wt · #seite.haendler]
    linebreak()
    text(size: 9pt)[#seite.ort · #seite.mahlzeit · #seite.bezugsort · #seite.arbeitsort · Bild #seite.seite]
    v(2mm)
    align(center, image(seite.datei, width: 100%, height: 150mm, fit: "contain"))
    v(2mm)
    text(size: 9pt)[
      Belegbetrag: #seite.belegbetrag #h(4mm)
      Korrigierter Betrag: #seite.korrigiert #h(4mm)
      Grund: #seite.korrektur_grund \
      Erstattung: #seite.erstattung #h(4mm)
      Quelle: #seite.quelle #h(4mm)
      Erkannt: #seite.erkannt \
      SHA-256: #seite.sha256 \
      Upload: #seite.upload #h(4mm)
      Letzte Änderung: #seite.geaendert
    ]
  }
}

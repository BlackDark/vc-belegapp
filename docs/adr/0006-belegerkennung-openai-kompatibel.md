# 0006 Belegerkennung über OpenAI-kompatible Chat Completions mit JSON-Schema
Status: angenommen (2026-10-08, Interview Runde 3)
Kontext: Günstiges Cloud-Vision-Modell als Standard, lokale Modelle müssen möglich sein.
Entscheidung: Interface `ReceiptExtractor`; Implementierung `openaicompat` (openai-go v3, `/chat/completions`, Bild als data-URL, `response_format: json_schema` mit Fallback `json_object`). Standard `gpt-5-mini`, Basis-URL und Modell per Env (Gemini-OpenAI-Endpoint, Ollama, vLLM, LM Studio). KI füllt nur vor; Nutzer bestätigt immer.
Konsequenzen: Kassenbelege gehen beim Cloud-Modell an Dritte (abschaltbar); Fehler blockieren die manuelle Erfassung nie. Details: SPEC.md §10.

# Zeitspur TODOs

## Bugs
- [x] **Idle-Detection: Double-Threshold Bug**
  - **Datei:** `internal/activity/engine.go` (in `func (e *Engine) tick(ctx context.Context) error`)
  - **Problem:** Die Desktop-Provider (GNOME/Freedesktop) warten bereits den konfigurierten `idleThreshold` (Standard: 5 Min), bevor sie den Status `ActivityIdle` zurückgeben. In der Engine wird bei Eintreffen von `ActivityIdle` jedoch *zusätzlich* geprüft, ob seit `lastActiveAt` bereits der `idleThreshold` vergangen ist (`now.Sub(e.lastActiveAt) >= e.idleThreshold`).
  - **Auswirkung:** Effektiv muss der Nutzer die doppelte Zeit (z.B. 10 Minuten) inaktiv sein, bevor Zeitspur auf "Idle" umschaltet. Jeder Mikrowackler in Minute 9 setzt den Timer komplett zurück und lässt große Zeitblöcke unerwartet als "Aktiv" stehen.
  - **Lösung:** Die Prüfung `now.Sub(e.lastActiveAt) >= e.idleThreshold` in `case ActivityIdle:` sollte entweder entfernt oder angepasst werden, da der Provider die Wartezeit schon erfüllt hat.


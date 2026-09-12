# Part 02: ComputerUse config

**Files:**
- Create: `internal/config/computeruse_config.go`
- Create: `internal/config/computeruse_config_test.go`
- Modify: `internal/config/ocodeconfig.go` (struct `OcodeConfig` near the `ImageGen ImageGenConfig` field around line 593; the load path where `imagegen` is decoded from `raw`, if one exists; the savers block near `SaveOcrConfig` around line 3087)

**Interfaces:**
- Produces: `config.ComputerUseConfig{Enabled bool}` with JSON tag `enabled`; field `OcodeConfig.ComputerUse` with JSON tag `computer_use`; `config.SaveComputerUseConfig(cfg ComputerUseConfig) error`; `config.DefaultComputerUseConfig() ComputerUseConfig` returning `Enabled: false`.

## Steps

- [ ] **Step 1: Read how `imagegen` is loaded and saved.** `grep -n "imagegen\|ImageGen" internal/config/ocodeconfig.go internal/config/imagegen_config.go`. Note whether the loader touches `raw["imagegen"]` explicitly or relies on struct tags. Mirror exactly that pattern.

- [ ] **Step 2: Write failing tests** in `computeruse_config_test.go`:
  - `TestComputerUseConfig_DefaultDisabled`: `DefaultComputerUseConfig().Enabled` is false.
  - `TestSaveComputerUseConfig_RoundTrip`: point the config dir at `t.TempDir()` the same way `internal/config` tests do for `SaveOcrConfig` (grep `SaveOcrConfig` in `internal/config/*_test.go` and copy the setup), write `Enabled: true`, reload with the package loader, assert `Ocode.ComputerUse.Enabled` is true and that an unrelated field written before (for example `Ocr.Enabled`) survived.

- [ ] **Step 3: Run** `go test ./internal/config -run 'ComputerUse' -v`. Expected: compile failure (undefined symbols).

- [ ] **Step 4: Implement.** New file holds the struct, default constructor and saver (saver uses `withOcodeConfigLock`, sets `cfg.ComputerUse`). Add the field to `OcodeConfig` next to `ImageGen`. Apply default in whatever function fills defaults for `Ocr`/`ImageGen` (grep `DefaultOcrConfig()` in `ocodeconfig.go`). If the loader handles `imagegen` explicitly in `raw`, add the same handling for `computer_use`.

- [ ] **Step 5: Run** `go test ./internal/config -run 'ComputerUse' -v` then `go test ./internal/config`. Expected: PASS, no regressions.

- [ ] **Step 6: Commit** `git add internal/config/computeruse_config.go internal/config/computeruse_config_test.go internal/config/ocodeconfig.go && git commit -m "feat(config): computer_use.enabled toggle"`.

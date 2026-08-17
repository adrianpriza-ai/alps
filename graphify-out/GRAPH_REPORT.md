# Graph Report - alps-dev  (2026-08-15)

## Corpus Check
- 210 files · ~333,245 words
- Verdict: corpus is large enough that graph structure adds value.

## Summary
- 3979 nodes · 12035 edges · 178 communities (159 shown, 19 thin omitted)
- Extraction: 82% EXTRACTED · 18% INFERRED · 0% AMBIGUOUS · INFERRED: 2144 edges (avg confidence: 0.8)
- Token cost: 0 input · 0 output

## Community Hubs (Navigation)
- Main Backend Core
- Flatpak Backend
- AUR Backend v1.0.3
- Fetch and Repo Operations
- AUR Backend v1.0.4
- AUR Backend Base
- AUR Backend v1.0.2
- Main Command Dispatch
- Package State Management
- Command Resolution
- Build Macros
- Build Macros v1.0.3
- AUR Backend v0.9.5
- AUR Backend v0.9.6
- AUR Search and Info
- Community 15
- Community 16
- Community 17
- Community 18
- Community 19
- Community 20
- Community 21
- Community 22
- Community 23
- Community 24
- Community 25
- Community 26
- Community 27
- Community 28
- Community 29
- Community 30
- Community 31
- Community 32
- Community 33
- Community 34
- Community 35
- Community 36
- Community 37
- Community 38
- Community 39
- Community 40
- Community 41
- Community 42
- Community 43
- Community 44
- Community 45
- Community 46
- Community 47
- Community 48
- Community 49
- Community 50
- Community 51
- Community 52
- Community 53
- Community 54
- Community 55
- Community 56
- Community 57
- Community 58
- Community 59
- Community 60
- Community 61
- Community 62
- Community 63
- Community 64
- Community 65
- Community 66
- Community 67
- Community 68
- Community 69
- Community 70
- Community 71
- Community 72
- Community 73
- Community 74
- Community 75
- Community 76
- Community 77
- Community 78
- Community 79
- Community 80
- Community 81
- Community 82
- Community 83
- Community 84
- Community 85
- Community 86
- Community 87
- Community 88
- Community 89
- Community 90
- Community 91
- Community 92
- Community 93
- Community 94
- Community 95
- Community 96
- Community 97
- Community 98
- Community 99
- Community 100
- Community 101
- Community 102
- Community 103
- Community 104
- Community 105
- Community 106
- Community 107
- Community 108
- Community 109
- Community 110
- Community 111
- Community 112
- Community 113
- Community 114
- Community 115
- Community 116
- Community 117
- Community 118
- Community 119
- Community 120
- Community 121
- Community 122
- Community 123
- Community 124
- Community 125
- Community 126
- Community 127
- Community 128
- Community 129
- Community 130
- Community 131
- Community 132
- Community 133
- Community 134
- Community 135
- Community 136
- Community 137
- Community 138
- Community 139
- Community 140
- Community 141
- Community 142
- Community 143
- Community 152
- Community 153
- Community 154
- Community 155
- Community 156
- Community 157
- Community 158
- Community 159
- Community 161
- Community 162
- Community 163
- Community 164
- Community 165
- Community 166
- Community 167
- Community 168
- Community 169
- Community 170
- Community 171
- Community 172
- Community 173
- Community 174
- Community 175
- Community 176
- Community 177

## God Nodes (most connected - your core abstractions)
1. `Msgf()` - 40 edges
2. `Msgf()` - 40 edges
3. `Msgf()` - 38 edges
4. `Msg()` - 36 edges
5. `Msg()` - 36 edges
6. `isTermux()` - 36 edges
7. `runRepo()` - 35 edges
8. `runRepo()` - 35 edges
9. `Msg()` - 35 edges
10. `isTermux()` - 35 edges

## Surprising Connections (you probably didn't know these)
- None detected (after removing legacy version trees)

## Import Cycles
- None detected.

## Hyperedges (group relationships)
- **GitHub Contribution Templates** — github_issue_template_bug_report_template, github_issue_template_feature_request_template, pr_template [EXTRACTED 1.00]

## Communities (178 total, 19 thin omitted)

### Community 0 - "Main Backend Core"
Cohesion: 0.06
Nodes (86): Backend, appendUniq(), Autoremove(), BuildExtraFlags(), BuildExtraFlagsExt(), Clean(), Detect(), DetectName() (+78 more)

### Community 1 - "Flatpak Backend"
Cohesion: 0.08
Nodes (76): Install(), IsAvailable(), List(), Remove(), Search(), Update(), appendAURNamesCache(), detectBackend() (+68 more)

### Community 2 - "AUR Backend v1.0.3"
Cohesion: 0.07
Nodes (74): sync.Mutex, aurCacheDir(), AURCacheRoot(), buildAndInstall(), buildInstallPlan(), BuildLocal(), checkDeps(), checkRequirements() (+66 more)

### Community 3 - "Fetch and Repo Operations"
Cohesion: 0.09
Nodes (72): runRepo(), CacheDir(), CachePath(), CacheStatus(), CleanCache(), downloadOnce(), downloadWithRetry(), ensureCacheDir() (+64 more)

### Community 4 - "AUR Backend v1.0.4"
Cohesion: 0.09
Nodes (67): aurCacheDir(), AURCacheRoot(), buildAndInstall(), buildInstallPlan(), BuildLocal(), checkDeps(), checkRequirements(), CleanCache() (+59 more)

### Community 5 - "AUR Backend Base"
Cohesion: 0.09
Nodes (65): aurCacheDir(), AURCacheRoot(), buildAndInstall(), buildInstallPlan(), BuildLocal(), checkDeps(), checkRequirements(), CleanCache() (+57 more)

### Community 6 - "AUR Backend v1.0.2"
Cohesion: 0.10
Nodes (60): aurCacheDir(), AURCacheRoot(), buildAndInstall(), buildInstallPlan(), BuildLocal(), checkDeps(), checkRequirements(), CleanCache() (+52 more)

### Community 7 - "Main Command Dispatch"
Cohesion: 0.10
Nodes (58): dispatchResolved(), ensureSudo(), fetchRepoEntry(), flatpakInstall(), flatpakList(), flatpakRemove(), flatpakSearch(), flatpakUpdate() (+50 more)

### Community 8 - "Package State Management"
Cohesion: 0.09
Nodes (54): InstalledRecord, buildScriptCmd(), checkDependencyGroup(), CheckUpdates(), containsCI(), detectDistro(), Find(), Macro (+46 more)

### Community 9 - "Command Resolution"
Cohesion: 0.12
Nodes (53): appendAURNamesCache(), detectBackend(), dispatchResolved(), ensureSudo(), flatpakInstall(), flatpakList(), flatpakRemove(), flatpakSearch() (+45 more)

### Community 10 - "Build Macros"
Cohesion: 0.13
Nodes (48): isMacOS(), combineMacroResult(), executeBashRun(), executeCreateUser(), ExecuteDeferredOps(), executeDisableService(), executeDownload(), executeEnableService() (+40 more)

### Community 11 - "Build Macros v1.0.3"
Cohesion: 0.14
Nodes (48): isMacOS(), combineMacroResult(), downloadSimple(), executeBashRun(), executeCreateUser(), ExecuteDeferredOps(), executeDisableService(), executeDownload() (+40 more)

### Community 12 - "AUR Backend v0.9.5"
Cohesion: 0.12
Nodes (45): aurCacheDir(), AURCacheRoot(), buildAndInstall(), buildInstallPlan(), BuildLocal(), checkDeps(), CleanCache(), cloneAUR() (+37 more)

### Community 13 - "AUR Backend v0.9.6"
Cohesion: 0.12
Nodes (45): aurCacheDir(), AURCacheRoot(), buildAndInstall(), buildInstallPlan(), BuildLocal(), checkDeps(), CleanCache(), cloneAUR() (+37 more)

### Community 14 - "AUR Search and Info"
Cohesion: 0.10
Nodes (46): GetInstalledAUR(), ListInstalledAUR(), PrintSearchResult(), Remove(), appendAURNamesCache(), ensureSudo(), fmtCmd(), isArch() (+38 more)

### Community 15 - "Community 15"
Cohesion: 0.12
Nodes (45): aurCacheDir(), AURCacheRoot(), buildAndInstall(), buildInstallPlan(), BuildLocal(), checkDeps(), CleanCache(), cloneAUR() (+37 more)

### Community 16 - "Community 16"
Cohesion: 0.10
Nodes (46): GetInstalledAUR(), ListInstalledAUR(), PrintSearchResult(), Remove(), AURNamesCachePath(), appendAURNamesCache(), ensureSudo(), fmtCmd() (+38 more)

### Community 17 - "Community 17"
Cohesion: 0.12
Nodes (45): CachePath(), CacheStatus(), downloadOnce(), downloadWithRetry(), ensureCacheDir(), FetchAndCache(), fetchRace(), getCacheDir() (+37 more)

### Community 18 - "Community 18"
Cohesion: 0.13
Nodes (47): runRepo(), resolveServer(), GenerateOwnedItems(), Entry, OwnedItem, NewMacroContext(), ValidateLine(), CheckUpdates() (+39 more)

### Community 19 - "Community 19"
Cohesion: 0.13
Nodes (47): runRepo(), resolveServer(), GenerateOwnedItems(), Entry, OwnedItem, NewMacroContext(), ValidateLine(), CheckUpdates() (+39 more)

### Community 20 - "Community 20"
Cohesion: 0.11
Nodes (46): isAllowedURL(), buildScriptCmd(), CheckUpdates(), containsCI(), detectDistro(), expandVars(), Find(), MacroContext (+38 more)

### Community 21 - "Community 21"
Cohesion: 0.10
Nodes (45): resolveServer(), GenerateOwnedItems(), Entry, OwnedItem, NewMacroContext(), cleanupTempFiles(), getBuildDir(), hasFakeroot() (+37 more)

### Community 22 - "Community 22"
Cohesion: 0.12
Nodes (43): buildScriptCmd(), CheckUpdates(), containsCI(), detectDistro(), expandVars(), Find(), MacroContext, Entry (+35 more)

### Community 23 - "Community 23"
Cohesion: 0.13
Nodes (41): CachePath(), CacheStatus(), downloadOnce(), downloadWithRetry(), ensureCacheDir(), FetchAndCache(), getCacheDir(), getCacheFile() (+33 more)

### Community 24 - "Community 24"
Cohesion: 0.13
Nodes (39): CacheStatus(), downloadOnce(), downloadWithRetry(), FetchAndCache(), hasValidEntries(), isCacheValid(), ReadCache(), sudoMkdir() (+31 more)

### Community 25 - "Community 25"
Cohesion: 0.14
Nodes (41): aurCacheDir(), AURCacheRoot(), buildAndInstall(), buildInstallPlan(), BuildLocal(), checkDeps(), CleanCache(), cloneAUR() (+33 more)

### Community 26 - "Community 26"
Cohesion: 0.12
Nodes (39): github.com/adrianpriza-ai/alps/aur.Package, GetInstalledAUR(), ListInstalledAUR(), PrintSearchResult(), Remove(), AURNamesCachePath(), appendAURNamesCache(), detectBackend() (+31 more)

### Community 27 - "Community 27"
Cohesion: 0.13
Nodes (38): ReadCache(), buildScriptCmd(), containsCI(), detectDistro(), expandVars(), Find(), Macro, MacroContext (+30 more)

### Community 28 - "Community 28"
Cohesion: 0.14
Nodes (36): appendAURNamesCache(), detectBackend(), detectRealBackend(), ensureSudo(), fmtCmd(), isArch(), isFilePath(), isTermux() (+28 more)

### Community 29 - "Community 29"
Cohesion: 0.16
Nodes (37): aurCacheDir(), AURCacheRoot(), buildAndInstall(), buildInstallPlan(), BuildLocal(), checkDeps(), CleanCache(), cloneAUR() (+29 more)

### Community 30 - "Community 30"
Cohesion: 0.15
Nodes (37): combineMacroResult(), executeBashRun(), executeCreateUser(), ExecuteDeferredOps(), executeDisableService(), executeDownload(), executeEnableService(), executeExtract() (+29 more)

### Community 31 - "Community 31"
Cohesion: 0.17
Nodes (35): runRepo(), resolveServer(), CheckUpdates(), containsCI(), detectDistro(), ensureSudo(), Find(), Entry (+27 more)

### Community 32 - "Community 32"
Cohesion: 0.17
Nodes (35): combineMacroResult(), executeBashRun(), executeCreateUser(), ExecuteDeferredOps(), executeDisableService(), executeDownload(), executeEnableService(), executeExtract() (+27 more)

### Community 33 - "Community 33"
Cohesion: 0.14
Nodes (35): resolveServer(), GenerateOwnedItems(), Entry, OwnedItem, NewMacroContext(), cleanupTempFiles(), getBuildDir(), Install() (+27 more)

### Community 34 - "Community 34"
Cohesion: 0.14
Nodes (34): resolveServer(), GenerateOwnedItems(), Entry, OwnedItem, NewMacroContext(), cleanupTempFiles(), getBuildDir(), hasFakeroot() (+26 more)

### Community 35 - "Community 35"
Cohesion: 0.13
Nodes (31): branchesForRef(), CacheDir(), CachePath(), CacheStatus(), CleanCache(), downloadOnce(), downloadWithRetry(), ensureCacheDir() (+23 more)

### Community 36 - "Community 36"
Cohesion: 0.16
Nodes (28): CacheStatus(), download(), FetchAndCache(), ReadCache(), containsCI(), detectDistro(), ensureSudo(), Find() (+20 more)

### Community 37 - "Community 37"
Cohesion: 0.20
Nodes (30): runRepo(), getInstalledFile(), resolveServer(), containsCI(), detectDistro(), ensureSudo(), Find(), Entry (+22 more)

### Community 38 - "Community 38"
Cohesion: 0.21
Nodes (31): executeCreateUser(), ExecuteDeferredOps(), executeDisableService(), executeEnableService(), executeExtract(), executeInstallBin(), executeInstallConf(), executeInstallDir() (+23 more)

### Community 39 - "Community 39"
Cohesion: 0.21
Nodes (31): executeCreateUser(), ExecuteDeferredOps(), executeDisableService(), executeEnableService(), executeExtract(), executeInstallBin(), executeInstallConf(), executeInstallDir() (+23 more)

### Community 40 - "Community 40"
Cohesion: 0.18
Nodes (30): github.com/adrianpriza-ai/alps/config.Config, github.com/adrianpriza-ai/alps/more.Entry, detectBackend(), detectRealBackend(), ensureSudo(), fmtCmd(), isArch(), isFilePath() (+22 more)

### Community 41 - "Community 41"
Cohesion: 0.18
Nodes (30): GetInstalledAUR(), ListInstalledAUR(), Remove(), appendAURNamesCache(), detectBackend(), detectRealBackend(), ensureSudo(), fmtCmd() (+22 more)

### Community 42 - "Community 42"
Cohesion: 0.13
Nodes (29): detectDistroID(), Level, isArchBased(), isDebianBased(), isFlatpakAvailable(), isSnapAvailable(), isTermux(), isTTY() (+21 more)

### Community 43 - "Community 43"
Cohesion: 0.15
Nodes (28): branchesForRef(), CacheDir(), CachePath(), CacheStatus(), CleanCache(), downloadOnce(), downloadWithRetry(), ensureCacheDir() (+20 more)

### Community 44 - "Community 44"
Cohesion: 0.18
Nodes (26): Command(), Ensure(), HasSu(), HasSudo(), IsRoot(), Command(), Ensure(), HasSu() (+18 more)

### Community 45 - "Community 45"
Cohesion: 0.15
Nodes (28): branchesForRef(), CacheDir(), CachePath(), CacheStatus(), CleanCache(), downloadOnce(), downloadWithRetry(), ensureCacheDir() (+20 more)

### Community 46 - "Community 46"
Cohesion: 0.16
Nodes (27): branchesForRef(), CacheDir(), CachePath(), CacheStatus(), CleanCache(), downloadOnce(), downloadWithRetry(), ensureCacheDir() (+19 more)

### Community 47 - "Community 47"
Cohesion: 0.18
Nodes (26): os/exec.Cmd, cleanupOwnedItems(), ensureSudo(), OwnedItem, removeDir(), removeFile(), removeFileWithSudo(), RemoveOwnedItems() (+18 more)

### Community 48 - "Community 48"
Cohesion: 0.18
Nodes (27): detectBackend(), detectRealBackend(), ensureSudo(), fmtCmd(), isArch(), isFilePath(), isTermux(), main() (+19 more)

### Community 49 - "Community 49"
Cohesion: 0.18
Nodes (27): detectBackend(), detectRealBackend(), ensureSudo(), fmtCmd(), isArch(), isFilePath(), isTermux(), main() (+19 more)

### Community 50 - "Community 50"
Cohesion: 0.17
Nodes (27): branchesForRef(), CacheDir(), CachePath(), CacheStatus(), CleanCache(), downloadOnce(), downloadWithRetry(), ensureCacheDir() (+19 more)

### Community 51 - "Community 51"
Cohesion: 0.17
Nodes (27): branchesForRef(), CacheDir(), CachePath(), CacheStatus(), CleanCache(), downloadOnce(), downloadWithRetry(), ensureCacheDir() (+19 more)

### Community 52 - "Community 52"
Cohesion: 0.15
Nodes (27): main(), resolveCmd(), detectDistroID(), isArchBased(), isDebianBased(), isFlatpakAvailable(), isSnapAvailable(), isTermux() (+19 more)

### Community 53 - "Community 53"
Cohesion: 0.15
Nodes (27): main(), resolveCmd(), detectDistroID(), isArchBased(), isDebianBased(), isFlatpakAvailable(), isSnapAvailable(), isTermux() (+19 more)

### Community 54 - "Community 54"
Cohesion: 0.19
Nodes (26): detectBackend(), detectRealBackend(), ensureSudo(), fmtCmd(), isArch(), isFilePath(), main(), needsSudo() (+18 more)

### Community 55 - "Community 55"
Cohesion: 0.18
Nodes (25): cleanupOwnedItems(), ensureSudo(), OwnedItem, removeDir(), removeFile(), removeFileWithSudo(), RemoveOwnedItems(), removeOwnedItemsVerbose() (+17 more)

### Community 56 - "Community 56"
Cohesion: 0.19
Nodes (25): isInstallOnlyMacro(), isRoot(), requireFakeroot(), categorizeMacro(), detectShell(), executeAfterEnv(), executeBuildEnv(), executeCommand() (+17 more)

### Community 57 - "Community 57"
Cohesion: 0.18
Nodes (25): aurCacheDir(), AURCacheRoot(), checkDeps(), DetectHelper(), Exists(), fetchRPC(), GetInstalledAUR(), Package (+17 more)

### Community 58 - "Community 58"
Cohesion: 0.15
Nodes (25): detectDistroID(), isArchBased(), isDebianBased(), isFlatpakAvailable(), isSnapAvailable(), isTermux(), isTTY(), padRight() (+17 more)

### Community 59 - "Community 59"
Cohesion: 0.12
Nodes (23): detectRealBackend(), fmtCmd(), needsSudo(), runPkgDefault(), runWithBackend(), runWithBackendFlags(), runWithBackendFlagsExt(), appendUniq() (+15 more)

### Community 60 - "Community 60"
Cohesion: 0.18
Nodes (22): aurCacheDir(), checkDeps(), DetectHelper(), Exists(), fetchRPC(), GetInstalledAUR(), Package, rpcResponse (+14 more)

### Community 61 - "Community 61"
Cohesion: 0.18
Nodes (22): aurCacheDir(), checkDeps(), DetectHelper(), Exists(), fetchRPC(), GetInstalledAUR(), Package, rpcResponse (+14 more)

### Community 62 - "Community 62"
Cohesion: 0.18
Nodes (22): aurCacheDir(), checkDeps(), DetectHelper(), Exists(), fetchRPC(), GetInstalledAUR(), Package, rpcResponse (+14 more)

### Community 63 - "Community 63"
Cohesion: 0.18
Nodes (22): aurCacheDir(), checkDeps(), DetectHelper(), Exists(), fetchRPC(), GetInstalledAUR(), Package, rpcResponse (+14 more)

### Community 64 - "Community 64"
Cohesion: 0.21
Nodes (24): resolveServer(), GenerateOwnedItems(), Entry, OwnedItem, NewMacroContext(), cleanupTempFiles(), executePurgePurgeStep(), executePurgeRemoveStep() (+16 more)

### Community 65 - "Community 65"
Cohesion: 0.25
Nodes (23): detectBackend(), detectRealBackend(), ensureSudo(), fmtCmd(), isFilePath(), needsSudo(), parseNotFound(), readLine() (+15 more)

### Community 66 - "Community 66"
Cohesion: 0.16
Nodes (22): detectDistroID(), Level, isArchBased(), isDebianBased(), isFlatpakAvailable(), isSnapAvailable(), isTTY(), Msg() (+14 more)

### Community 67 - "Community 67"
Cohesion: 0.16
Nodes (22): detectDistroID(), Level, isArchBased(), isDebianBased(), isFlatpakAvailable(), isSnapAvailable(), isTTY(), Msg() (+14 more)

### Community 68 - "Community 68"
Cohesion: 0.16
Nodes (22): detectDistroID(), Level, isArchBased(), isDebianBased(), isFlatpakAvailable(), isSnapAvailable(), isTTY(), Msg() (+14 more)

### Community 69 - "Community 69"
Cohesion: 0.24
Nodes (20): aurInstalledCmd(), aurNamesCmd(), cacheDir(), cacheFile(), cmdDesc(), detectBackend(), detectDistroID(), effectiveCmds() (+12 more)

### Community 70 - "Community 70"
Cohesion: 0.22
Nodes (21): runRepo(), CachePath(), CacheStatus(), download(), FetchAndCache(), ReadCache(), baseName(), containsCI() (+13 more)

### Community 71 - "Community 71"
Cohesion: 0.16
Nodes (22): isTermux(), main(), printDiagnostic(), resolveCmd(), detectDistroID(), isArchBased(), isDebianBased(), isFlatpakAvailable() (+14 more)

### Community 72 - "Community 72"
Cohesion: 0.17
Nodes (22): main(), resolveCmd(), detectDistroID(), isArchBased(), isDebianBased(), isFlatpakAvailable(), isSnapAvailable(), isTermux() (+14 more)

### Community 73 - "Community 73"
Cohesion: 0.24
Nodes (21): aurInstalledCmd(), AURNamesCachePath(), aurNamesCmd(), cacheDir(), cacheFile(), cmdDesc(), detectBackend(), detectDistroID() (+13 more)

### Community 74 - "Community 74"
Cohesion: 0.24
Nodes (21): aurInstalledCmd(), AURNamesCachePath(), aurNamesCmd(), cacheDir(), cacheFile(), cmdDesc(), detectBackend(), detectDistroID() (+13 more)

### Community 75 - "Community 75"
Cohesion: 0.24
Nodes (21): aurInstalledCmd(), AURNamesCachePath(), aurNamesCmd(), cacheDir(), cacheFile(), cmdDesc(), detectBackend(), detectDistroID() (+13 more)

### Community 76 - "Community 76"
Cohesion: 0.24
Nodes (21): aurInstalledCmd(), AURNamesCachePath(), aurNamesCmd(), cacheDir(), cacheFile(), cmdDesc(), detectBackend(), detectDistroID() (+13 more)

### Community 77 - "Community 77"
Cohesion: 0.25
Nodes (19): Command(), CommandModern(), CommandSudoOnly(), Ensure(), EnsureModern(), EnsureSudoOnly(), HasDoas(), HasPkexec() (+11 more)

### Community 78 - "Community 78"
Cohesion: 0.26
Nodes (20): aurInstalledCmd(), AURNamesCachePath(), aurNamesCmd(), cacheDir(), cacheFile(), cmdDesc(), detectBackend(), detectDistroID() (+12 more)

### Community 79 - "Community 79"
Cohesion: 0.26
Nodes (20): aurInstalledCmd(), AURNamesCachePath(), aurNamesCmd(), cacheDir(), cacheFile(), cmdDesc(), detectBackend(), detectDistroID() (+12 more)

### Community 80 - "Community 80"
Cohesion: 0.26
Nodes (20): aurInstalledCmd(), aurNamesCmd(), cacheDir(), cacheFile(), cmdDesc(), detectBackend(), detectDistroID(), effectiveCmds() (+12 more)

### Community 81 - "Community 81"
Cohesion: 0.18
Nodes (21): main(), detectDistroID(), isArchBased(), isDebianBased(), isFlatpakAvailable(), isSnapAvailable(), isTermux(), isTTY() (+13 more)

### Community 82 - "Community 82"
Cohesion: 0.26
Nodes (20): aurInstalledCmd(), aurNamesCmd(), cacheDir(), cacheFile(), cmdDesc(), detectBackend(), detectDistroID(), effectiveCmds() (+12 more)

### Community 83 - "Community 83"
Cohesion: 0.18
Nodes (21): main(), detectDistroID(), isArchBased(), isDebianBased(), isFlatpakAvailable(), isSnapAvailable(), isTermux(), isTTY() (+13 more)

### Community 84 - "Community 84"
Cohesion: 0.26
Nodes (20): applyAliasFilter(), cacheDir(), cacheFile(), cmdDesc(), detectBackend(), detectDistroID(), effectiveCmds(), genBash() (+12 more)

### Community 85 - "Community 85"
Cohesion: 0.26
Nodes (20): applyAliasFilter(), cacheDir(), cacheFile(), cmdDesc(), detectBackend(), detectDistroID(), effectiveCmds(), genBash() (+12 more)

### Community 86 - "Community 86"
Cohesion: 0.14
Nodes (18): detectBackend(), detectRealBackend(), needsSudo(), splitFlagsAll(), appendUniq(), BuildExtraFlags(), BuildExtraFlagsExt(), Detect() (+10 more)

### Community 87 - "Community 87"
Cohesion: 0.19
Nodes (20): ensureLibDir(), getInstalledFile(), getLibDir(), CheckUpdates(), UpdateSummary, isRemoteSource(), ListInstalled(), ListStale() (+12 more)

### Community 88 - "Community 88"
Cohesion: 0.14
Nodes (13): appendUniq(), BuildExtraFlags(), BuildExtraFlagsExt(), Detect(), DetectName(), Backend, Flags, init() (+5 more)

### Community 89 - "Community 89"
Cohesion: 0.19
Nodes (18): detectDistroID(), isArchBased(), isDebianBased(), isFlatpakAvailable(), isSnapAvailable(), isTTY(), PrintAliases(), printConfigPath() (+10 more)

### Community 90 - "Community 90"
Cohesion: 0.24
Nodes (17): testing.T, ParseMacro(), detectDistroVersion(), TestDetectDistroVersion(), TestExecuteManifestFakeroot(), TestExpandMacrosDisver(), TestExpandVarsDisver(), TestIsAllowedURL() (+9 more)

### Community 91 - "Community 91"
Cohesion: 0.25
Nodes (15): runSnap(), Command(), CommandSudoOnly(), Ensure(), EnsureSudoOnly(), HasSu(), HasSudo(), IsRoot() (+7 more)

### Community 92 - "Community 92"
Cohesion: 0.25
Nodes (17): CachePath(), CacheStatus(), downloadOnce(), downloadWithRetry(), ensureCacheDir(), FetchAndCache(), fetchRace(), getCacheDir() (+9 more)

### Community 93 - "Community 93"
Cohesion: 0.25
Nodes (15): runSnap(), Command(), CommandSudoOnly(), Ensure(), EnsureSudoOnly(), HasSu(), HasSudo(), IsRoot() (+7 more)

### Community 94 - "Community 94"
Cohesion: 0.18
Nodes (15): detectBackend(), detectRealBackend(), appendUniq(), BuildExtraFlags(), BuildExtraFlagsExt(), Detect(), DetectName(), Backend (+7 more)

### Community 95 - "Community 95"
Cohesion: 0.18
Nodes (15): detectBackend(), detectRealBackend(), appendUniq(), BuildExtraFlags(), BuildExtraFlagsExt(), Detect(), DetectName(), Backend (+7 more)

### Community 96 - "Community 96"
Cohesion: 0.17
Nodes (15): appendUniq(), BuildExtraFlags(), BuildExtraFlagsExt(), CommandSupported(), Detect(), DetectName(), Backend, Flags (+7 more)

### Community 97 - "Community 97"
Cohesion: 0.16
Nodes (16): isKnownMacro(), ParseMacro(), detectDistroVersion(), expandVars(), TestExpandMacrosDisver(), TestExpandVarsDisver(), TestIsAllowedURL(), TestParseDeps() (+8 more)

### Community 98 - "Community 98"
Cohesion: 0.24
Nodes (15): detectDistroID(), Level, isArchBased(), isDebianBased(), isFlatpakAvailable(), isSnapAvailable(), Msg(), Msgf() (+7 more)

### Community 99 - "Community 99"
Cohesion: 0.27
Nodes (16): CachePath(), CacheStatus(), downloadOnce(), downloadWithRetry(), ensureCacheDir(), FetchAndCache(), fetchRace(), getCacheDir() (+8 more)

### Community 100 - "Community 100"
Cohesion: 0.18
Nodes (14): appendUniq(), BuildExtraFlags(), BuildExtraFlagsExt(), Detect(), DetectName(), Backend, Flags, init() (+6 more)

### Community 101 - "Community 101"
Cohesion: 0.19
Nodes (11): io.Reader, strings.Builder, time.Time, downloadWithProgress(), formatSize(), formatTime(), getTerminalWidth(), progressReader (+3 more)

### Community 102 - "Community 102"
Cohesion: 0.23
Nodes (13): Remove(), runSnap(), Command(), Ensure(), HasSu(), HasSudo(), IsRoot(), Install() (+5 more)

### Community 103 - "Community 103"
Cohesion: 0.27
Nodes (14): ensureLibDir(), getInstalledFile(), getLibDir(), writeCacheFile(), TestMacOS(), Entry, InstalledRecord, OwnedItem (+6 more)

### Community 104 - "Community 104"
Cohesion: 0.29
Nodes (13): main(), resolveAlias(), detectDistroID(), isArchBased(), isDebianBased(), isFlatpakAvailable(), isSnapAvailable(), PrintAliases() (+5 more)

### Community 105 - "Community 105"
Cohesion: 0.36
Nodes (12): cmdDesc(), detectBackend(), detectDistroID(), effectiveCmds(), genBash(), Generate(), genFish(), genZsh() (+4 more)

### Community 106 - "Community 106"
Cohesion: 0.36
Nodes (12): cmdDesc(), detectBackend(), detectDistroID(), effectiveCmds(), genBash(), Generate(), genFish(), genZsh() (+4 more)

### Community 107 - "Community 107"
Cohesion: 0.36
Nodes (12): cmdDesc(), detectBackend(), detectDistroID(), effectiveCmds(), genBash(), Generate(), genFish(), genZsh() (+4 more)

### Community 108 - "Community 108"
Cohesion: 0.35
Nodes (12): getInstalledFile(), writeCacheFile(), GetInstalled(), Entry, InstalledRecord, OwnedItem, MarkInstalled(), MarkInstalledEntry() (+4 more)

### Community 109 - "Community 109"
Cohesion: 0.35
Nodes (12): getInstalledFile(), writeCacheFile(), GetInstalled(), Entry, InstalledRecord, OwnedItem, MarkInstalled(), MarkInstalledEntry() (+4 more)

### Community 110 - "Community 110"
Cohesion: 0.23
Nodes (12): ParseMacro(), detectDistroVersion(), TestDetectDistroVersion(), TestExpandMacrosDisver(), TestExpandVarsDisver(), TestParseDuplicateEntries(), TestParseMacroArgsInsideBraces(), TestParseMacroArgsOutsideBraces() (+4 more)

### Community 111 - "Community 111"
Cohesion: 0.31
Nodes (8): github.com/adrianpriza-ai/alps/more.RemoteRef, fetchRepoEntry(), defaultHost(), RemoteRef, IsRemoteURL(), ParseRemoteURL(), ParseSource(), providerFromHost()

### Community 112 - "Community 112"
Cohesion: 0.29
Nodes (10): ParseMacro(), detectDistroVersion(), TestDetectDistroVersion(), TestExpandMacrosDisver(), TestExpandVarsDisver(), TestParseMacroArgsInsideBraces(), TestParseMacroArgsOutsideBraces(), TestParseMacroNoArgs() (+2 more)

### Community 113 - "Community 113"
Cohesion: 0.33
Nodes (10): ensureLibDir(), getInstalledFile(), getLibDir(), Entry, InstalledRecord, OwnedItem, MarkInstalled(), MarkInstalledEntry() (+2 more)

### Community 114 - "Community 114"
Cohesion: 0.31
Nodes (8): Install(), IsAvailable(), List(), Remove(), Search(), Update(), resolveSubCmd(), runFlatpak()

### Community 115 - "Community 115"
Cohesion: 0.33
Nodes (10): ensureLibDir(), getLibDir(), OwnedItem, isTermux(), removeDir(), removeFile(), RemoveOwnedItems(), removeService() (+2 more)

### Community 116 - "Community 116"
Cohesion: 0.33
Nodes (10): ensureLibDir(), getLibDir(), OwnedItem, isTermux(), removeDir(), removeFile(), RemoveOwnedItems(), removeService() (+2 more)

### Community 117 - "Community 117"
Cohesion: 0.24
Nodes (10): GetInstalledAUR(), PacmanConfig, ListInstalledAUR(), PrintSearchResult(), ReadPacmanConf(), appendAURNamesCache(), isArch(), runAUR() (+2 more)

### Community 118 - "Community 118"
Cohesion: 0.39
Nodes (6): defaultHost(), RemoteRef, IsRemoteURL(), ParseRemoteURL(), ParseSource(), providerFromHost()

### Community 119 - "Community 119"
Cohesion: 0.44
Nodes (8): globalConfigPath(), Config, Style, isTTY(), Load(), parseFile(), unescape(), userConfigPath()

### Community 120 - "Community 120"
Cohesion: 0.44
Nodes (8): globalConfigPath(), Config, Style, isTTY(), Load(), parseFile(), unescape(), userConfigPath()

### Community 121 - "Community 121"
Cohesion: 0.36
Nodes (7): Install(), IsAvailable(), List(), Remove(), Search(), Update(), runFlatpak()

### Community 122 - "Community 122"
Cohesion: 0.44
Nodes (8): globalConfigPath(), Config, Style, isTTY(), Load(), parseFile(), unescape(), userConfigPath()

### Community 123 - "Community 123"
Cohesion: 0.44
Nodes (8): globalConfigPath(), Config, Style, isTTY(), Load(), parseFile(), unescape(), userConfigPath()

### Community 124 - "Community 124"
Cohesion: 0.44
Nodes (8): globalConfigPath(), Config, Style, isTTY(), Load(), parseFile(), unescape(), userConfigPath()

### Community 125 - "Community 125"
Cohesion: 0.44
Nodes (8): globalConfigPath(), Config, Style, isTTY(), Load(), parseFile(), unescape(), userConfigPath()

### Community 126 - "Community 126"
Cohesion: 0.36
Nodes (7): Install(), IsAvailable(), List(), Remove(), Search(), Update(), runFlatpak()

### Community 127 - "Community 127"
Cohesion: 0.44
Nodes (8): globalConfigPath(), Config, Style, isTTY(), Load(), parseFile(), unescape(), userConfigPath()

### Community 128 - "Community 128"
Cohesion: 0.61
Nodes (8): Command(), CommandSudoOnly(), Ensure(), EnsureSudoOnly(), HasSu(), HasSudo(), IsRoot(), isTermux()

### Community 129 - "Community 129"
Cohesion: 0.44
Nodes (8): globalConfigPath(), Config, Style, isTTY(), Load(), parseFile(), unescape(), userConfigPath()

### Community 130 - "Community 130"
Cohesion: 0.36
Nodes (7): Install(), IsAvailable(), List(), Remove(), Search(), Update(), runFlatpak()

### Community 131 - "Community 131"
Cohesion: 0.44
Nodes (8): globalConfigPath(), Config, Style, isTTY(), Load(), parseFile(), unescape(), userConfigPath()

### Community 132 - "Community 132"
Cohesion: 0.36
Nodes (7): Install(), IsAvailable(), List(), Remove(), Search(), Update(), runFlatpak()

### Community 133 - "Community 133"
Cohesion: 0.39
Nodes (6): defaultHost(), RemoteRef, IsRemoteURL(), ParseRemoteURL(), ParseSource(), providerFromHost()

### Community 134 - "Community 134"
Cohesion: 0.61
Nodes (8): Command(), CommandSudoOnly(), Ensure(), EnsureSudoOnly(), HasSu(), HasSudo(), IsRoot(), isTermux()

### Community 135 - "Community 135"
Cohesion: 0.44
Nodes (8): globalConfigPath(), Config, Style, isTTY(), Load(), parseFile(), unescape(), userConfigPath()

### Community 136 - "Community 136"
Cohesion: 0.36
Nodes (7): Install(), IsAvailable(), List(), Remove(), Search(), Update(), runFlatpak()

### Community 137 - "Community 137"
Cohesion: 0.39
Nodes (6): defaultHost(), RemoteRef, IsRemoteURL(), ParseRemoteURL(), ParseSource(), providerFromHost()

### Community 138 - "Community 138"
Cohesion: 0.61
Nodes (8): Command(), CommandSudoOnly(), Ensure(), EnsureSudoOnly(), HasSu(), HasSudo(), IsRoot(), isTermux()

### Community 139 - "Community 139"
Cohesion: 0.44
Nodes (8): globalConfigPath(), Config, Style, isTTY(), Load(), parseFile(), unescape(), userConfigPath()

### Community 140 - "Community 140"
Cohesion: 0.39
Nodes (6): defaultHost(), RemoteRef, IsRemoteURL(), ParseRemoteURL(), ParseSource(), providerFromHost()

### Community 141 - "Community 141"
Cohesion: 0.39
Nodes (6): defaultHost(), RemoteRef, IsRemoteURL(), ParseRemoteURL(), ParseSource(), providerFromHost()

### Community 142 - "Community 142"
Cohesion: 0.44
Nodes (8): globalConfigPath(), Config, Style, isTTY(), Load(), parseFile(), unescape(), userConfigPath()

### Community 143 - "Community 143"
Cohesion: 0.50
Nodes (8): globalConfigPath(), Config, Style, isTTY(), Load(), parseFile(), unescape(), userConfigPath()

### Community 152 - "Community 152"
Cohesion: 0.25
Nodes (6): Install(), IsAvailable(), List(), Remove(), Search(), Update()

### Community 153 - "Community 153"
Cohesion: 0.25
Nodes (6): Install(), IsAvailable(), List(), Remove(), Search(), Update()

### Community 154 - "Community 154"
Cohesion: 0.48
Nodes (7): cleanupOwnedItems(), OwnedItem, removeDir(), removeFile(), RemoveOwnedItems(), removeOwnedItemsVerbose(), removeService()

### Community 155 - "Community 155"
Cohesion: 0.48
Nodes (7): cleanupOwnedItems(), OwnedItem, removeDir(), removeFile(), RemoveOwnedItems(), removeOwnedItemsVerbose(), removeService()

### Community 156 - "Community 156"
Cohesion: 0.73
Nodes (5): Command(), Ensure(), HasSu(), HasSudo(), IsRoot()

### Community 157 - "Community 157"
Cohesion: 0.73
Nodes (5): Command(), Ensure(), HasSu(), HasSudo(), IsRoot()

## Knowledge Gaps
- **38 isolated node(s):** `$schema`, `plugin`, `github.com/adrianpriza-ai/alps`, `fetchResult`, `github.com/adrianpriza-ai/alps` (+33 more)
  These have ≤1 connection - possible missing edges or undocumented components.
- **19 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `runAUR()` connect `AUR Search and Info` to `Community 40`, `Community 81`, `AUR Backend v0.9.6`, `Community 94`?**
  _High betweenness centrality (0.031) - this node is a cross-community bridge._
- **Why does `runAUR()` connect `Flatpak Backend` to `Community 40`, `AUR Backend v1.0.3`, `Community 58`?**
  _High betweenness centrality (0.026) - this node is a cross-community bridge._
- **Why does `Command()` connect `Community 77` to `Community 33`, `AUR Backend v1.0.4`, `Command Resolution`, `Build Macros`, `Community 155`, `Community 47`, `Community 59`?**
  _High betweenness centrality (0.025) - this node is a cross-community bridge._
- **Are the 36 inferred relationships involving `Msgf()` (e.g. with `fetchRepoEntry()` and `flatpakInstall()`) actually correct?**
  _`Msgf()` has 36 INFERRED edges - model-reasoned connections that need verification._
- **Are the 36 inferred relationships involving `Msgf()` (e.g. with `fetchRepoEntry()` and `flatpakInstall()`) actually correct?**
  _`Msgf()` has 36 INFERRED edges - model-reasoned connections that need verification._
- **Are the 34 inferred relationships involving `Msgf()` (e.g. with `fetchRepoEntry()` and `flatpakInstall()`) actually correct?**
  _`Msgf()` has 34 INFERRED edges - model-reasoned connections that need verification._
- **What connects `$schema`, `plugin`, `github.com/adrianpriza-ai/alps` to the rest of the system?**
  _38 weakly-connected nodes found - possible documentation gaps or missing edges._
# ast-index Rules

## Mandatory Search Rules

1. **ALWAYS use ast-index FIRST** for any code search task
2. **NEVER duplicate results** — if ast-index found usages/implementations, that IS the complete answer
3. **DO NOT run grep "for completeness"** after ast-index returns results
4. **Use grep/Search ONLY when:**
   - ast-index returns empty results
   - Searching for regex patterns (ast-index uses literal match)
   - Searching for string literals inside code (`"some text"`)
   - Searching in comments content

## Why ast-index

ast-index is 17-69x faster than grep (1-10ms vs 200ms-3s) and returns structured, accurate results.

## Command Reference

| Task | Command | Time |
|------|---------|------|
| Universal search | `ast-index search "query"` | ~10ms |
| Find class/component | `ast-index class "ComponentName"` | ~1ms |
| Find symbol | `ast-index symbol "SymbolName"` | ~1ms |
| Find usages | `ast-index usages "SymbolName"` | ~8ms |
| Find implementations | `ast-index implementations "Interface"` | ~5ms |
| Call hierarchy | `ast-index call-tree "function" --depth 3` | ~1s |
| Find callers | `ast-index callers "functionName"` | ~1s |
| Module deps | `ast-index deps "module-name"` | ~10ms |
| File outline | `ast-index outline "File.tsx"` | ~1ms |

## Go-Specific Commands

| Task | Command |
|------|---------|
| Find struct | `ast-index class "StructName"` |
| Find interface | `ast-index class "InterfaceName"` |
| Find implementations | `ast-index implementations "InterfaceName"` |
| Find function | `ast-index symbol "FuncName"` |
| Find callers | `ast-index callers "FuncName"` |
| Module deps | `ast-index deps "module-path"` |

## TypeScript/JavaScript-Specific Commands

| Task | Command |
|------|---------|
| Find React components | `ast-index class "Component"` |
| Find React hooks | `ast-index search "use" --kind function` |
| Find interfaces | `ast-index class "Props"` |
| Find types | `ast-index symbol "DTO"` |

## Index Management

- `ast-index rebuild` — Full reindex (run once after clone)
- `ast-index update` — After git pull/merge
- `ast-index stats` — Show index statistics

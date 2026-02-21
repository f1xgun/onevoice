# Anti-Patterns

Common mistakes to avoid in this codebase. Many of these are caught by linters, but some require awareness.

## Go Backend

### Creating helpers when shared utils exist

```go
// BAD: Writing a new encrypt function
func encryptToken(token string) (string, error) { ... }

// GOOD: Use the shared encryptor in pkg/crypto
encrypted, err := s.encryptor.Encrypt([]byte(token))
```

Check `pkg/` before creating utility code. Shared packages:
- `pkg/crypto` — encryption/decryption
- `pkg/domain` — models, errors, interfaces
- `pkg/llm` — LLM routing, providers
- `pkg/a2a` — agent-to-agent protocol
- `pkg/logger` — structured logging

### Ignoring errors

```go
// BAD: Silently ignoring errors (caught by errcheck)
result, _ := doSomething()

// GOOD: Handle or explicitly document why it's safe to ignore
result, err := doSomething()
if err != nil {
    return fmt.Errorf("do something: %w", err)
}
```

### Using os.Setenv in tests

```go
// BAD: Leaks env changes to other tests
func TestConfig(t *testing.T) {
    os.Setenv("PORT", "9090")
    // ...
}

// GOOD: Auto-restores on test cleanup
func TestConfig(t *testing.T) {
    t.Setenv("PORT", "9090")
    // ...
}
```

### Creating context.Background() in business logic

```go
// BAD: Ignores caller's cancellation
func (s *Service) Process() error {
    ctx := context.Background()
    return s.repo.Save(ctx, data)
}

// GOOD: Accept and propagate context
func (s *Service) Process(ctx context.Context) error {
    return s.repo.Save(ctx, data)
}
```

`context.Background()` is only acceptable in `main()`, tests, and fire-and-forget goroutines (e.g., async billing).

### Hand-rolling validation

```go
// BAD: Manual validation
if req.Email == "" {
    return errors.New("email required")
}
if !strings.Contains(req.Email, "@") {
    return errors.New("invalid email")
}

// GOOD: Use validator library
type CreateUserRequest struct {
    Email string `json:"email" validate:"required,email"`
    Name  string `json:"name"  validate:"required,min=1,max=100"`
}
err := validate.Struct(req)
```

### Skipping layers

```go
// BAD: Handler directly calls repository
func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
    user, err := h.userRepo.GetByID(r.Context(), id)
    // ...
}

// GOOD: Handler → Service → Repository
func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
    user, err := h.userService.GetByID(r.Context(), id)
    // ...
}
```

### Unwrapped errors

```go
// BAD: Loses error chain
return errors.New("failed to save user")

// GOOD: Wraps with context
return fmt.Errorf("save user: %w", err)
```

## Frontend (Next.js / React)

### Inline styles instead of Tailwind

```tsx
// BAD
<div style={{ padding: '16px', marginTop: '8px' }}>

// GOOD
<div className="p-4 mt-2">
```

### Manual form state instead of react-hook-form

```tsx
// BAD: Manual state + validation
const [name, setName] = useState('');
const [error, setError] = useState('');

const handleSubmit = () => {
  if (!name) setError('Required');
};

// GOOD: react-hook-form + zod
const { register, handleSubmit, formState: { errors } } = useForm({
  resolver: zodResolver(schema),
});
```

### Global state in useState

```tsx
// BAD: Auth state in local component
const [user, setUser] = useState(null);
const [token, setToken] = useState(null);

// GOOD: Zustand store
const { user, token } = useAuthStore();
```

### Missing type imports

```tsx
// BAD (caught by consistent-type-imports)
import { User } from '@/types';

// GOOD
import type { User } from '@/types';
```

### Using const for components

```tsx
// BAD: Harder to debug (anonymous in stack traces)
const ChatMessage = ({ message }: Props) => { ... };

// GOOD: Named function declaration
function ChatMessage({ message }: Props) { ... }
```

import { createSignal } from "solid-js";
import { api } from "../api";
import { setNeedsAuth } from "../store";

export default function AuthForm() {
  const [email, setEmail] = createSignal("");
  const [password, setPassword] = createSignal("");
  const [showPassword, setShowPassword] = createSignal(false);
  const [error, setError] = createSignal("");

  async function handleSubmit(e: Event) {
    e.preventDefault();
    const eVal = email().trim();
    const pVal = password();
    if (!eVal || !pVal) return;

    setError("");
    try {
      await api.authenticate(eVal, pVal);
      setNeedsAuth(false);
    } catch (err: unknown) {
      const apiErr = err as { status?: number };
      setError(apiErr.status === 401 ? "Invalid email or password" : "Connection failed");
    }
  }

  return (
    <article class="auth-container">
      <header>
        <h2>Freezer Inventory</h2>
        <p>Sign in to continue.</p>
      </header>
      <form onSubmit={handleSubmit}>
        {error() && <p class="auth-error">{error()}</p>}
        <label>
          Email
          <input
            type="email"
            value={email()}
            onInput={(e) => setEmail((e.target as HTMLInputElement).value)}
            placeholder="you@example.com"
            autofocus
          />
        </label>
        <label>
          Password
          <div class="password-field">
            <input
              type={showPassword() ? "text" : "password"}
              value={password()}
              onInput={(e) => setPassword((e.target as HTMLInputElement).value)}
              placeholder="Enter password..."
            />
            <button
              type="button"
              class="outline password-toggle"
              onClick={() => setShowPassword((v) => !v)}
              aria-label={showPassword() ? "Hide password" : "Show password"}
            >
              {showPassword() ? "Hide" : "Show"}
            </button>
          </div>
        </label>
        <button type="submit">Sign In</button>
      </form>
    </article>
  );
}

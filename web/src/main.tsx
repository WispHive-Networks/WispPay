import { Alert, Button, CircularProgress, CssBaseline, TextField, ThemeProvider, createTheme } from '@mui/material';
import { createRoot } from 'react-dom/client';
import { BrowserRouter, Link, Route, Routes } from 'react-router';
import { FormEvent, useEffect, useState } from 'react';
import { PublicInvoicePage, WorkspacePage } from './workspace';
import './styles.css';

const theme = createTheme({
  palette: {
    mode: 'light',
    primary: { main: '#176b5b' },
    background: { default: '#f5f4ef', paper: '#fffefa' },
  },
  shape: { borderRadius: 6 },
  typography: { fontFamily: 'Georgia, "Times New Roman", serif' },
});

type Identity = { id: string; businessId: string; businessName: string; email: string };

function FoundationPage() {
  const [identity, setIdentity] = useState<Identity | null>(null);
  const [csrfToken, setCsrfToken] = useState('');
  const [registering, setRegistering] = useState(false);
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState('');

  useEffect(() => {
    let active = true;
    Promise.all([
      fetch('/api/auth/csrf', { credentials: 'same-origin' }).then(async (response) => {
        if (!response.ok) throw new Error('Security token could not be initialized.');
        return response.json() as Promise<{ csrfToken: string }>;
      }),
      fetch('/api/auth/me', { credentials: 'same-origin' }),
    ]).then(async ([csrf, session]) => {
      if (!active) return;
      setCsrfToken(csrf.csrfToken);
      if (session.ok) setIdentity(await session.json() as Identity);
    }).catch(() => {
      if (active) setError('WispPay could not reach the application API. Refresh to retry.');
    }).finally(() => {
      if (active) setBusy(false);
    });
    return () => { active = false; };
  }, []);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setError('');
    const form = new FormData(event.currentTarget);
    const body = registering
      ? { businessName: form.get('businessName'), email: form.get('email'), password: form.get('password') }
      : { email: form.get('email'), password: form.get('password') };
    try {
      const response = await fetch(`/api/auth/${registering ? 'register' : 'login'}`, {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify(body),
      });
      const result = await response.json();
      if (!response.ok) throw new Error(result.error ?? 'Sign-in failed.');
      setIdentity(result as Identity);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Sign-in failed.');
    } finally {
      setBusy(false);
    }
  }

  async function logout() {
    setBusy(true);
    try {
      const response = await fetch('/api/auth/logout', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'X-CSRF-Token': csrfToken },
      });
      if (!response.ok) throw new Error('Sign-out failed.');
      setIdentity(null);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Sign-out failed.');
    } finally {
      setBusy(false);
    }
  }

  if (identity) return <WorkspacePage identity={identity} csrfToken={csrfToken} onLogout={logout} />;

  return (
    <main className="foundation-page">
      <header className="topbar">
        <Link className="wordmark" to="/">WispPay<span> / WORKSPACE</span></Link>
        {identity ? <Button size="small" variant="outlined" onClick={logout} disabled={busy}>Sign out</Button> : <span className="environment-label">SECURE WORKSPACE</span>}
      </header>
      <section className="intro auth-intro" aria-labelledby="page-title">
          <p className="eyebrow">BUSINESS PAYMENTS, IN ONE PLACE</p>
          <h1 id="page-title">{registering ? 'Create your workspace.' : 'Welcome back.'}</h1>
          <p className="intro-copy">{registering ? 'Set up a secure workspace for your business.' : 'Sign in to manage your business workspace.'}</p>
          <form className="auth-form" onSubmit={submit}>
            {registering && <TextField name="businessName" label="Business name" autoComplete="organization" required slotProps={{ htmlInput: { minLength: 2, maxLength: 120 } }} />}
            <TextField name="email" label="Work email" type="email" autoComplete="email" required slotProps={{ htmlInput: { maxLength: 254 } }} />
            <TextField name="password" label="Password" type="password" autoComplete={registering ? 'new-password' : 'current-password'} required slotProps={{ htmlInput: { minLength: registering ? 12 : 1, maxLength: 128 } }} />
            {error && <Alert severity="error">{error}</Alert>}
            <Button type="submit" variant="contained" size="large" disabled={busy || !csrfToken}>
              {busy ? <CircularProgress size={20} color="inherit" /> : registering ? 'Create workspace' : 'Sign in'}
            </Button>
            <Button type="button" variant="text" onClick={() => { setRegistering(!registering); setError(''); }} disabled={busy}>
              {registering ? 'I already have an account' : 'Create a business workspace'}
            </Button>
          </form>
      </section>
      <footer>WispPay <span>Secure invoicing and payment links</span></footer>
    </main>
  );
}

function App() {
  return (
    <ThemeProvider theme={theme}>
      <CssBaseline />
      <BrowserRouter>
        <Routes>
          <Route path="/i/:token" element={<PublicInvoicePage />} />
          <Route path="*" element={<FoundationPage />} />
        </Routes>
      </BrowserRouter>
    </ThemeProvider>
  );
}

createRoot(document.getElementById('root')!).render(<App />);
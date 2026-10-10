// Cognito authentication via aws-amplify v6. Amplify is used ONLY for auth: sign-in, token
// refresh and sign-out. API calls are plain fetch() with the ID token attached (see api.ts).
import { Amplify } from 'aws-amplify';
import { fetchAuthSession, signIn, signOut } from 'aws-amplify/auth';

export type Group = 'Patient' | 'Doctor' | 'HospitalAdmin';

export type User = {
  userId: string; // Cognito sub (= patientId for patients)
  email: string;
  groups: Group[];
  hospitalId: string | null;
};

Amplify.configure({
  Auth: {
    Cognito: {
      userPoolId: import.meta.env.VITE_USER_POOL_ID ?? '',
      userPoolClientId: import.meta.env.VITE_USER_POOL_CLIENT_ID ?? '',
      loginWith: { email: true },
    },
  },
});

// Returns the current ID token (Amplify refreshes it automatically when expired), or null.
export async function getIdToken(): Promise<string | null> {
  const session = await fetchAuthSession();
  return session.tokens?.idToken?.toString() ?? null;
}

// Reads the signed-in user from the ID token's claims. The UI uses these only to decide what
// to SHOW. The backend enforces the real access rules from the same (verified) claims.
export async function currentUser(): Promise<User | null> {
  const session = await fetchAuthSession();
  const payload = session.tokens?.idToken?.payload;
  if (!payload) return null;
  return {
    userId: String(payload.sub),
    email: String(payload.email ?? ''),
    groups: (payload['cognito:groups'] as Group[] | undefined) ?? [],
    hospitalId: (payload['custom:hospital_id'] as string | undefined) ?? null,
  };
}

export async function login(email: string, password: string): Promise<void> {
  const { isSignedIn, nextStep } = await signIn({ username: email, password });
  if (!isSignedIn) {
    // The seed script sets permanent passwords, so no extra steps are expected.
    throw new Error(`Additional sign-in step required: ${nextStep.signInStep}`);
  }
}

export async function logout(): Promise<void> {
  await signOut();
}

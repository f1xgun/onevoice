import { RegistrationPageContent } from './RegistrationPageContent';

interface RegisterPageProps {
  searchParams: Promise<{ invited?: string | string[] }>;
}

export const dynamic = 'force-dynamic';

export default async function RegisterPage({ searchParams }: RegisterPageProps) {
  const { invited } = await searchParams;
  return <RegistrationPageContent invited={invited} />;
}

import { useParams } from 'react-router-dom';
import { useContact } from '@/hooks/queries/contacts';
import { Skeleton } from '@/design';
import { usePageTitle } from '@/hooks';
import { ContactForm } from './ContactForm';

export function ContactEditPage() {
  const { id } = useParams<{ id: string }>();
  const isNew = id === 'new';
  const q = useContact(isNew ? '' : (id ?? ''));
  usePageTitle(isNew ? 'New contact' : 'Edit contact');

  return (
    <div className="h-full flex flex-col bg-surface">
      <header className="px-4 py-3 border-b border-line">
        <h1 className="text-lg font-semibold">{isNew ? 'New contact' : 'Edit contact'}</h1>
      </header>
      <div className="flex-1 overflow-y-auto scroll-thin">
        {!isNew && q.isLoading && <div className="p-6"><Skeleton className="h-40 w-full" /></div>}
        {!isNew && q.isError && <p className="p-6 text-danger text-sm">Could not load contact.</p>}
        {!isNew && q.data && <ContactForm contact={q.data} />}
        {isNew && <ContactForm />}
      </div>
    </div>
  );
}

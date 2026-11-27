import { useEffect, useState } from 'react';
import { useSetSsoConfig, useSsoConfig } from '@/hooks/queries/security';
import { Button, Field, Input, Select, Skeleton, Switch } from '@/design';
import { Icon } from '@/design/icons';
import { errorMessage } from '@/lib/errors';
import { toast } from '@/stores/toast';

export function SsoTab() {
  const q = useSsoConfig(true);
  const set = useSetSsoConfig();
  const [provider, setProvider] = useState<'oidc' | 'saml'>('oidc');
  const [metadataUrl, setMetadataUrl] = useState('');
  const [enabled, setEnabled] = useState(false);

  useEffect(() => {
    if (q.data) {
      setProvider(q.data.provider ?? 'oidc');
      setMetadataUrl(q.data.metadata_url ?? '');
      setEnabled(q.data.enabled);
    }
  }, [q.data]);

  async function save() {
    try {
      await set.mutateAsync({ provider, metadata_url: metadataUrl.trim(), enabled });
      toast('success', 'SSO configuration saved');
    } catch (e) {
      toast('error', 'Could not save', errorMessage(e));
    }
  }

  if (q.isLoading) return <Skeleton className="h-40 w-full" />;

  return (
    <div className="max-w-2xl space-y-4">
      <p className="text-sm text-ink-soft">
        Enforce single sign-on so members of your organisation authenticate through your identity provider
        (Microsoft Entra ID, Google Workspace, Okta, Keycloak, or any OIDC/SAML 2.0 provider).
      </p>

      <div className="border border-line rounded-lg p-4 space-y-3">
        <Field label="Protocol">
          <Select value={provider} onChange={(e) => setProvider(e.target.value as 'oidc' | 'saml')}>
            <option value="oidc">OpenID Connect (OIDC)</option>
            <option value="saml">SAML 2.0</option>
          </Select>
        </Field>

        <Field
          label={provider === 'oidc' ? 'OIDC discovery URL' : 'SAML metadata URL'}
          hint={provider === 'oidc'
            ? 'e.g. https://login.microsoftonline.com/{tenant}/v2.0/.well-known/openid-configuration'
            : 'URL of the SAML metadata XML provided by your IdP.'}
        >
          <Input value={metadataUrl} onChange={(e) => setMetadataUrl(e.target.value)} placeholder="https://…" />
        </Field>

        <div className="flex items-center gap-2">
          <Switch checked={enabled} onChange={setEnabled} label="Enforce SSO for all members of this organisation" />
        </div>

        {enabled && (
          <div className="flex items-start gap-2 p-3 border border-warning/40 bg-warning-soft rounded text-sm">
            <Icon name="alert-triangle" size={14} className="mt-0.5 shrink-0 text-warning" />
            <p>
              When enforced, password sign-in will be disabled for all members except the organisation owner.
              Make sure your SSO configuration works before enabling enforcement.
            </p>
          </div>
        )}

        <div className="flex justify-end pt-2 border-t border-line">
          <Button variant="primary" onClick={save} loading={set.isPending}>
            Save SSO configuration
          </Button>
        </div>
      </div>
    </div>
  );
}

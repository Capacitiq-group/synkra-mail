// MigrationRoute — the IMAP migration assistant (phase 11): source config →
// live folder probe with editable mapping → job creation → progress dashboard
// with per-message attention list, pause/resume/cancel.
import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '@/lib/api';
import { errorMessage } from '@/lib/errors';
import { toast } from '@/stores/toast';
import { usePageTitle } from '@/hooks';
import { Button, Field, Input, Select, Skeleton } from '@/design';
import { Panel } from '@/components/Banner';
import type { MigrationFolder, MigrationJobView } from '@/lib/types';

export interface MigrationSource { host: string; port: number; security: string; username: string; password: string }

export function MigrationRoute() {
  usePageTitle('Migration');
  const [phase, setPhase] = useState<'config' | 'mapping' | 'jobs'>(
    new URLSearchParams(location.search).get('job') ? 'jobs' : 'config');
  const [folders, setFolders] = useState<MigrationFolder[] | null>(null);
  const [source, setSource] = useState<MigrationSource | null>(null);
  const [activeJob, setActiveJob] = useState<string | null>(
    new URLSearchParams(location.search).get('job'));

  return (
    <div className="p-4 md:p-6 max-w-4xl mx-auto space-y-4">
      <div>
        <h1 className="text-lg font-semibold">IMAP migration</h1>
        <p className="text-sm text-ink-soft">Move mail from another provider into Synkra. Messages arrive through the normal delivery pipeline — spam analysis, dedup and blobs behave exactly like real mail.</p>
      </div>

      {phase === 'config' && (
        <SourceConfigStep onProbed={(f, s) => { setFolders(f); setSource(s); setPhase('mapping'); }} />
      )}
      {phase === 'mapping' && folders && source && (
        <FolderMappingStep folders={folders} source={source} onDone={(jobId) => { setActiveJob(jobId); setPhase('jobs'); }} onBack={() => setPhase('config')} />
      )}
      {phase === 'jobs' && (
        <ProgressDashboard jobId={activeJob} onNew={() => { setActiveJob(null); setPhase('config'); }} />
      )}
    </div>
  );
}

function SourceConfigStep({ onProbed }: { onProbed: (f: MigrationFolder[], s: MigrationSource) => void }) {
  const [host, setHost] = useState('');
  const [port, setPort] = useState('993');
  const [security, setSecurity] = useState('tls');
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const probe = useMutation({
    mutationFn: () => api.probeMigrationSource({ host, port: Number(port), security, username, password }),
    onSuccess: (f) => onProbed(f, { host, port: Number(port), security, username, password }),
    onError: (e) => toast('error', 'Source unreachable', errorMessage(e)),
  });
  return (
    <Panel title="Source mailbox">
      <div className="grid md:grid-cols-2 gap-3">
        <Field label="IMAP host"><Input value={host} onChange={(e) => setHost(e.target.value)} placeholder="imap.oldprovider.co.za" /></Field>
        <Field label="Port"><Input value={port} onChange={(e) => setPort(e.target.value)} /></Field>
        <Field label="Security">
          <Select value={security} onChange={(e) => setSecurity(e.target.value)}>
            <option value="tls">TLS (implicit, 993)</option>
            <option value="starttls">STARTTLS (143)</option>
            <option value="none">None (dev only)</option>
          </Select>
        </Field>
        <Field label="Username"><Input value={username} onChange={(e) => setUsername(e.target.value)} /></Field>
        <Field label="Password"><Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} /></Field>
      </div>
      <p className="text-xs text-ink-soft">The password is sealed with the platform key at rest and only unsealed inside the worker.</p>
      <div className="flex justify-end">
        <Button variant="primary" disabled={!host || !username || !password || probe.isPending} onClick={() => probe.mutate()}>
          {probe.isPending ? 'Connecting…' : 'Connect and list folders'}
        </Button>
      </div>
    </Panel>
  );
}

function FolderMappingStep({ folders, source, onDone, onBack }: {
  folders: MigrationFolder[]; source: MigrationSource; onDone: (jobId: string) => void; onBack: () => void;
}) {
  const qc = useQueryClient();
  const [map, setMap] = useState<Record<string, string>>(
    () => Object.fromEntries(folders.filter((f) => f.total > 0).map((f) => [f.name, f.dest])));
  const mailboxesQ = useQuery({ queryKey: ['mailboxes'], queryFn: api.mailboxes, staleTime: 30_000 });
  const [mailboxId, setMailboxId] = useState('');
  const [sourcePassword, setSourcePassword] = useState('');

  const create = useMutation({
    mutationFn: () => api.createMigrationJob({
      mailbox_id: mailboxId,
      source: { ...source, password: sourcePassword },
      folder_map: map,
    }),
    onSuccess: (job) => { toast('success', 'Migration queued'); qc.invalidateQueries({ queryKey: ['migrations'] }); onDone(job.id); },
    onError: (e) => toast('error', 'Could not start', errorMessage(e)),
  });

  return (
    <Panel title="Folder mapping">
      <div className="space-y-2">
        {folders.filter((f) => f.total > 0).map((f) => (
          <div key={f.name} className="flex items-center gap-2 text-sm">
            <span className="w-48 truncate font-medium">{f.name}</span>
            <span className="text-ink-soft">{f.total} msgs →</span>
            <Select
              value={map[f.name] ?? f.dest}
              onChange={(e) => setMap({ ...map, [f.name]: e.target.value })}
            >
              {['INBOX', 'Sent', 'Drafts', 'Trash', 'Spam', 'Archive'].map((d) => (
                <option key={d} value={d}>{d}</option>
              ))}
              <option value={f.name}>{f.name} (as-is)</option>
            </Select>
          </div>
        ))}
        {folders.every((f) => f.total === 0) && <p className="text-sm text-ink-soft">The source has no messages.</p>}
      </div>

      <div className="border-t border-line pt-3 grid md:grid-cols-2 gap-3">
        <Field label="Destination mailbox">
          <Select value={mailboxId} onChange={(e) => setMailboxId(e.target.value)}>
            <option value="">Choose…</option>
            {mailboxesQ.data?.map((m) => <option key={m.id} value={m.id}>{m.address}</option>)}
          </Select>
        </Field>
        <Field label="Re-enter source password" hint={`${source.username} @ ${source.host}:${source.port} (${source.security})`}>
          <Input type="password" value={sourcePassword} onChange={(e) => setSourcePassword(e.target.value)} />
        </Field>
      </div>

      <div className="flex justify-between">
        <Button variant="ghost" onClick={onBack}>Back</Button>
        <Button
          variant="primary"
          disabled={!mailboxId || !sourcePassword || create.isPending}
          onClick={() => create.mutate()}
        >
          {create.isPending ? 'Starting…' : 'Start migration'}
        </Button>
      </div>
    </Panel>
  );
}

function ProgressDashboard({ jobId, onNew }: { jobId: string | null; onNew: () => void }) {
  const qc = useQueryClient();
  const jobsQ = useQuery({ queryKey: ['migrations'], queryFn: () => api.listMigrationJobs(), refetchInterval: 5000 });
  const [selected, setSelected] = useState<string | null>(jobId);
  const active = selected ?? jobsQ.data?.[0]?.id ?? null;
  const jobQ = useQuery({
    queryKey: ['migration', active],
    queryFn: () => api.getMigrationJob(active!, 'failed', 0),
    enabled: !!active,
    refetchInterval: 5000,
  });
  const act = useMutation({
    mutationFn: ({ id, action }: { id: string; action: 'pause' | 'resume' | 'cancel' }) => api.migrationAction(id, action),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['migrations'] }),
    onError: (e) => toast('error', 'Action failed', errorMessage(e)),
  });
  const del = useMutation({
    mutationFn: (id: string) => api.deleteMigrationJob(id),
    onSuccess: () => { toast('success', 'Job deleted'); qc.invalidateQueries({ queryKey: ['migrations'] }); onNew(); },
    onError: (e) => toast('error', 'Delete failed', errorMessage(e)),
  });

  if (!jobsQ.data?.length) {
    return (
      <Panel title="Migrations">
        <p className="text-sm text-ink-soft">No migrations yet.</p>
        <Button variant="primary" onClick={onNew}>Start a migration</Button>
      </Panel>
    );
  }
  const job: MigrationJobView | undefined = jobsQ.data.find((j) => j.id === active) ?? jobsQ.data[0];
  const pct = job.total_messages > 0 ? Math.round(((job.migrated + job.failed) / job.total_messages) * 100) : 0;
  const terminal = ['completed', 'failed', 'cancelled'].includes(job.state);

  return (
    <div className="space-y-4">
      {jobsQ.data.length > 1 && (
        <Select value={job.id} onChange={(e) => { setSelected(e.target.value); qc.invalidateQueries({ queryKey: ['migration'] }); }}>
          {jobsQ.data.map((j) => <option key={j.id} value={j.id}>{j.id.slice(0, 8)} — {j.state}</option>)}
        </Select>
      )}

      <Panel title={`Progress — ${job.state}`}>
        <div className="flex justify-between text-sm">
          <span>{job.migrated} migrated · {job.failed} failed · {job.total_messages} total</span>
          <span className="font-medium">{pct}%</span>
        </div>
        <div className="h-2 bg-canvas rounded overflow-hidden">
          <div className="h-full bg-accent transition-all" style={{ width: `${pct}%` }} />
        </div>
        {job.last_error && <p className="text-xs text-danger">{job.last_error}</p>}
        <div className="flex flex-wrap gap-2">
          {!terminal && job.state === 'running' && <Button variant="ghost" onClick={() => act.mutate({ id: job.id, action: 'pause' })}>Pause</Button>}
          {!terminal && (job.state === 'paused' || job.state === 'queued') && <Button variant="primary" onClick={() => act.mutate({ id: job.id, action: 'resume' })}>Resume</Button>}
          {!terminal && <Button variant="ghost" onClick={() => act.mutate({ id: job.id, action: 'cancel' })}>Cancel</Button>}
          {terminal && <Button variant="ghost" onClick={() => del.mutate(job.id)}>Delete job</Button>}
          <Button variant="ghost" onClick={onNew}>New migration</Button>
        </div>
      </Panel>

      <Panel title="Attention list (failed messages)">
        {jobQ.isLoading && <Skeleton className="h-16 w-full" />}
        {jobQ.data?.items.length === 0 && <p className="text-sm text-ink-soft">No failures — everything migrated.</p>}
        <div className="space-y-2">
          {jobQ.data?.items.map((it) => (
            <div key={it.id} className="border border-line rounded p-2 text-sm">
              <div className="flex justify-between gap-2">
                <span className="font-medium truncate">{it.subject || '(no subject)'}</span>
                <span className="text-xs text-ink-soft">{it.folder}/{it.uid} · {(it.size_bytes / 1024).toFixed(0)} KB · attempt {it.attempts}</span>
              </div>
              {it.error && <p className="text-xs text-danger mt-1">{it.error}</p>}
            </div>
          ))}
        </div>
      </Panel>
    </div>
  );
}

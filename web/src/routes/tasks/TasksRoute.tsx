import { useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { useTaskLists, useTasks, useUpdateTask, useDeleteTask, useCreateTaskList, useDeleteTaskList } from '@/hooks/queries/tasks';
import { Button, ConfirmDialog, EmptyState, Field, Input, Menu, MenuItem, Modal, Skeleton, Switch, Tabs } from '@/design';
import { Icon } from '@/design/icons';
import { formatDateTime, relativeTime } from '@/lib/time';
import { errorMessage } from '@/lib/errors';
import { toast } from '@/stores/toast';
import { usePageTitle, useMediaQuery } from '@/hooks';
import { TaskModal } from './TaskModal';
import type { Task, TaskList } from '@/lib/types';

export function TasksRoute() {
  usePageTitle('Tasks');
  const isDesktop = useMediaQuery('(min-width: 768px)');
  const listsQ = useTaskLists();
  const [params, setParams] = useSearchParams();
  const activeListId = params.get('list') ?? listsQ.data?.[0]?.id ?? '';
  const tasksQ = useTasks(activeListId || undefined);
  const update = useUpdateTask();
  const del = useDeleteTask();
  const delList = useDeleteTaskList();
  const createList = useCreateTaskList();
  const [showCompleted, setShowCompleted] = useState(false);
  const [createTaskOpen, setCreateTaskOpen] = useState(false);
  const [createListOpen, setCreateListOpen] = useState(false);
  const [newListName, setNewListName] = useState('');
  const [editTask, setEditTask] = useState<Task | null>(null);
  const [deleteTask, setDeleteTask] = useState<Task | null>(null);
  const [deleteListTarget, setDeleteListTarget] = useState<TaskList | null>(null);

  const lists = listsQ.data ?? [];
  const activeList = lists.find((l) => l.id === activeListId);

  const filtered = useMemo(() => {
    const all = tasksQ.data ?? [];
    return showCompleted ? all : all.filter((t) => !t.completed);
  }, [tasksQ.data, showCompleted]);

  const grouped = useMemo(() => {
    const today = new Date(); today.setHours(23, 59, 59, 999);
    const buckets: { key: 'overdue' | 'today' | 'week' | 'later' | 'none'; label: string; tasks: Task[] }[] = [
      { key: 'overdue', label: 'Overdue', tasks: [] },
      { key: 'today', label: 'Today', tasks: [] },
      { key: 'week', label: 'This week', tasks: [] },
      { key: 'later', label: 'Later', tasks: [] },
      { key: 'none', label: 'No due date', tasks: [] },
    ];
    const map = new Map(buckets.map((b) => [b.key, b]));
    const weekEnd = new Date(today.getTime() + 7 * 86400000);
    for (const t of filtered) {
      let key: 'overdue' | 'today' | 'week' | 'later' | 'none' = 'none';
      if (t.due_at) {
        const due = new Date(t.due_at);
        if (due < new Date() && !t.completed) key = 'overdue';
        else if (due <= today) key = 'today';
        else if (due <= weekEnd) key = 'week';
        else key = 'later';
      }
      map.get(key)!.tasks.push(t);
    }
    return buckets.filter((b) => b.tasks.length > 0);
  }, [filtered]);

  function selectList(id: string) {
    const next = new URLSearchParams(params);
    next.set('list', id);
    setParams(next, { replace: true });
  }

  return (
    <div className="h-full flex bg-surface">
      {isDesktop && (
        <aside className="w-56 shrink-0 border-r border-line flex flex-col">
          <div className="p-3 border-b border-line">
            <Button variant="primary" className="w-full" onClick={() => setCreateTaskOpen(true)}>
              <Icon name="plus" size={14} /> New task
            </Button>
          </div>
          <nav className="flex-1 overflow-y-auto scroll-thin p-2" aria-label="Task lists">
            <h3 className="px-2 pb-1 text-xs uppercase text-ink-mute">Lists</h3>
            {lists.map((l) => (
              <div key={l.id} className="group flex items-center">
                <button
                  onClick={() => selectList(l.id)}
                  className={`flex-1 text-left px-2 py-1.5 rounded text-sm flex items-center gap-2 ${
                    activeListId === l.id ? 'bg-canvas font-medium' : 'hover:bg-canvas'
                  }`}
                >
                  <span className="w-2 h-2 rounded-full shrink-0" style={{ backgroundColor: l.color }} />
                  <span className="truncate">{l.name}</span>
                </button>
                {l.shared ? (
                  <span className="text-xs text-ink-mute px-1">shared</span>
                ) : (
                  <Menu trigger={<button className="opacity-0 group-hover:opacity-100 p-1 text-ink-mute" aria-label="List actions"><Icon name="more" size={12} /></button>}>
                    <MenuItem danger onClick={() => setDeleteListTarget(l)}>Delete list</MenuItem>
                  </Menu>
                )}
              </div>
            ))}
            <button
              onClick={() => setCreateListOpen(true)}
              className="w-full text-left px-2 py-1.5 text-xs text-ink-soft hover:text-ink hover:bg-canvas rounded mt-2"
            >
              + New list
            </button>
          </nav>
        </aside>
      )}

      <main className="flex-1 min-w-0 flex flex-col">
        <header className="flex items-center gap-2 px-4 py-3 border-b border-line">
          <h1 className="text-lg font-semibold flex-1 truncate">{activeList?.name ?? 'All tasks'}</h1>
          <Switch checked={showCompleted} onChange={setShowCompleted} label="Show completed" />
          <Button variant="primary" onClick={() => setCreateTaskOpen(true)}>
            <Icon name="plus" size={14} /> New task
          </Button>
        </header>

        {!isDesktop && lists.length > 0 && (
          <div className="border-b border-line overflow-x-auto">
            <Tabs items={lists.map((l) => ({ id: l.id, label: l.name }))} value={activeListId} onChange={selectList} />
          </div>
        )}

        <div className="flex-1 overflow-y-auto scroll-thin">
          {tasksQ.isLoading && <div className="p-4 space-y-2">{Array.from({ length: 4 }).map((_, i) => <Skeleton key={i} className="h-14 w-full" />)}</div>}
          {!tasksQ.isLoading && filtered.length === 0 && (
            <EmptyState
              icon={<Icon name="check-circle" size={28} />}
              title={showCompleted ? 'No tasks yet' : 'All done!'}
              description={showCompleted ? 'Create a task to track work.' : 'Nothing left in this list.'}
              action={<Button variant="primary" onClick={() => setCreateTaskOpen(true)}>New task</Button>}
            />
          )}

          <div className="max-w-3xl mx-auto p-3 space-y-4">
            {grouped.map((g) => (
              <section key={g.key}>
                <h2 className={`text-xs uppercase mb-2 ${g.key === 'overdue' ? 'text-danger' : 'text-ink-mute'}`}>
                  {g.label} <span className="normal-case">({g.tasks.length})</span>
                </h2>
                <ul className="space-y-1">
                  {g.tasks.map((t) => (
                    <li key={t.id} className="flex items-start gap-2 p-2 rounded hover:bg-canvas group">
                      <input
                        type="checkbox"
                        checked={t.completed}
                        onChange={(e) => update.mutate({ id: t.id, patch: { completed: e.target.checked } })}
                        className="mt-1 rounded border-line"
                        aria-label={t.completed ? 'Mark incomplete' : 'Mark complete'}
                      />
                      <button onClick={() => setEditTask(t)} className="flex-1 text-left min-w-0">
                        <p className={`text-sm ${t.completed ? 'line-through text-ink-mute' : ''}`}>{t.title}</p>
                        <div className="text-xs text-ink-soft flex items-center gap-2 mt-0.5">
                          {t.due_at && (
                            <span className={g.key === 'overdue' ? 'text-danger' : ''}>
                              Due {formatDateTime(t.due_at)}
                            </span>
                          )}
                          {t.source_message_id && <span className="text-ink-mute">· from email</span>}
                          {t.assigned_to && <span className="text-ink-mute">· assigned</span>}
                        </div>
                      </button>
                      <Menu trigger={<button className="p-1 text-ink-mute opacity-0 group-hover:opacity-100" aria-label="Task actions"><Icon name="more" size={14} /></button>}>
                        <MenuItem onClick={() => setEditTask(t)}>Edit</MenuItem>
                        <MenuItem danger onClick={() => setDeleteTask(t)}>Delete</MenuItem>
                      </Menu>
                    </li>
                  ))}
                </ul>
              </section>
            ))}
          </div>
        </div>
      </main>

      <TaskModal
        open={createTaskOpen || !!editTask}
        onClose={() => { setCreateTaskOpen(false); setEditTask(null); }}
        task={editTask ?? undefined}
        defaultListId={activeListId || lists[0]?.id}
        lists={lists}
      />

      <Modal
        open={createListOpen}
        onClose={() => setCreateListOpen(false)}
        title="New task list"
        footer={
          <>
            <Button variant="secondary" onClick={() => setCreateListOpen(false)}>Cancel</Button>
            <Button variant="primary" loading={createList.isPending} disabled={!newListName.trim()} onClick={async () => {
              try {
                const l = await createList.mutateAsync({ name: newListName.trim(), color: '#0f766e', shared: false });
                setNewListName('');
                setCreateListOpen(false);
                selectList(l.id);
              } catch (e) { toast('error', 'Could not create list', errorMessage(e)); }
            }}>Create</Button>
          </>
        }
      >
        <Field label="List name">
          <Input value={newListName} onChange={(e) => setNewListName(e.target.value)} autoFocus />
        </Field>
      </Modal>

      <ConfirmDialog
        open={!!deleteTask}
        onCancel={() => setDeleteTask(null)}
        onConfirm={async () => {
          if (!deleteTask) return;
          try { await del.mutateAsync(deleteTask.id); toast('success', 'Task deleted'); setDeleteTask(null); }
          catch (e) { toast('error', 'Could not delete', errorMessage(e)); }
        }}
        title="Delete this task?"
        confirmLabel="Delete"
        danger
        busy={del.isPending}
      />

      <ConfirmDialog
        open={!!deleteListTarget}
        onCancel={() => setDeleteListTarget(null)}
        onConfirm={async () => {
          if (!deleteListTarget) return;
          try {
            await delList.mutateAsync(deleteListTarget.id);
            toast('success', 'List deleted');
            setDeleteListTarget(null);
            if (deleteListTarget.id === activeListId) selectList(lists[0]?.id ?? '');
          } catch (e) { toast('error', 'Could not delete', errorMessage(e)); }
        }}
        title={`Delete "${deleteListTarget?.name}"?`}
        description="All tasks in this list will be deleted. This cannot be undone."
        confirmLabel="Delete list"
        danger
        busy={delList.isPending}
      />
    </div>
  );
}

// Suppress unused import.
void relativeTime;

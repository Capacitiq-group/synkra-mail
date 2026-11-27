import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '@/lib/api';
import type { Task, TaskList } from '@/lib/types';

const listsKey = () => ['task-lists'] as const;
const tasksKey = (listId?: string) => ['tasks', listId ?? ''] as const;

export function useTaskLists() {
  return useQuery({ queryKey: listsKey(), queryFn: api.listTaskLists, staleTime: 60_000 });
}

export function useCreateTaskList() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: Omit<TaskList, 'id'>) => api.createTaskList(input),
    onSuccess: () => qc.invalidateQueries({ queryKey: listsKey() }),
  });
}

export function useDeleteTaskList() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.deleteTaskList(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: listsKey() }),
  });
}

export function useTasks(listId?: string) {
  return useQuery({
    queryKey: tasksKey(listId),
    queryFn: () => api.listTasks(listId),
    staleTime: 15_000,
  });
}

export function useCreateTask() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: Omit<Task, 'id' | 'created_at' | 'completed' | 'completed_at'>) => api.createTask(input),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['tasks'] }),
  });
}

export function useUpdateTask() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, patch }: { id: string; patch: Partial<Task> }) => api.updateTask(id, patch),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['tasks'] }),
  });
}

export function useDeleteTask() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.deleteTask(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['tasks'] }),
  });
}

export function useCreateTaskFromMessage() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: Parameters<typeof api.createTaskFromMessage>[0]) => api.createTaskFromMessage(input),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['tasks'] }),
  });
}

export type { Task, TaskList };

// Shared test fixture for roles UI tests.
//
// Keep this deterministic — exact ids/names/permissions are asserted on
// across tests. The catalog mirrors the shape returned by
// `GET /api/v1/permissions` and the role list shape from
// `GET /api/v1/businesses/{id}/roles`.

import type { PermissionGroup, Role } from '@/lib/schemas';

// 3 groups x 4 leaves = 12 total permissions. Catalog is intentionally
// compact so tests can hand-tune actor permission sets per scenario.
export const CATALOG: PermissionGroup[] = [
  {
    resource: 'business',
    permissions: [
      { name: 'business.read', description: 'Видеть профиль' },
      { name: 'business.update', description: 'Редактировать профиль' },
      { name: 'business.delete', description: 'Удалить организацию' },
      { name: 'business.transfer_ownership', description: 'Передать владение' },
    ],
  },
  {
    resource: 'roles',
    permissions: [
      { name: 'roles.read', description: 'Видеть роли' },
      { name: 'roles.create', description: 'Создавать роли' },
      { name: 'roles.update', description: 'Изменять роли' },
      { name: 'roles.delete', description: 'Удалять роли' },
    ],
  },
  {
    resource: 'members',
    permissions: [
      { name: 'members.read', description: 'Видеть участников' },
      { name: 'members.invite', description: 'Приглашать' },
      { name: 'members.remove', description: 'Удалять участников' },
      { name: 'members.update_role', description: 'Менять роли' },
    ],
  },
];

export const OWNER_ID = '00000000-0000-0000-0000-000000000001';
export const ADMIN_ID = '00000000-0000-0000-0000-000000000002';
export const EDITOR_ID = '00000000-0000-0000-0000-000000000003';
export const VIEWER_ID = '00000000-0000-0000-0000-000000000004';

const ALL_PERMS = [
  'business.read',
  'business.update',
  'business.delete',
  'business.transfer_ownership',
  'roles.read',
  'roles.create',
  'roles.update',
  'roles.delete',
  'members.read',
  'members.invite',
  'members.remove',
  'members.update_role',
];

export const SYSTEM_ROLES: Role[] = [
  {
    id: OWNER_ID,
    business_id: null,
    name: 'owner',
    description: '',
    permissions: ALL_PERMS,
    is_system: true,
    member_count: 0,
  },
  {
    id: ADMIN_ID,
    business_id: null,
    name: 'admin',
    description: '',
    permissions: [
      'business.read',
      'business.update',
      'roles.read',
      'roles.create',
      'roles.update',
      'roles.delete',
      'members.read',
      'members.invite',
      'members.remove',
      'members.update_role',
    ],
    is_system: true,
    member_count: 0,
  },
  {
    id: EDITOR_ID,
    business_id: null,
    name: 'editor',
    description: '',
    permissions: ['business.read', 'roles.read', 'members.read'],
    is_system: true,
    member_count: 0,
  },
  {
    id: VIEWER_ID,
    business_id: null,
    name: 'viewer',
    description: '',
    permissions: ['business.read', 'roles.read', 'members.read'],
    is_system: true,
    member_count: 0,
  },
];

export const MARKETING_ROLE: Role = {
  id: '11111111-1111-1111-1111-111111111111',
  business_id: 'biz-1',
  name: 'Marketing',
  description: 'Команда маркетинга',
  permissions: ['business.read', 'roles.read', 'members.read'],
  is_system: false,
  member_count: 5,
};

export const EMPTY_CUSTOM_ROLE: Role = {
  id: '22222222-2222-2222-2222-222222222222',
  business_id: 'biz-1',
  name: 'Empty Role',
  description: 'Без участников',
  permissions: ['business.read'],
  is_system: false,
  member_count: 0,
};

export const ANALYTICS_ROLE: Role = {
  id: '33333333-3333-3333-3333-333333333333',
  business_id: 'biz-1',
  name: 'Analytics',
  description: 'Аналитика и отчёты',
  permissions: ['business.read', 'members.read'],
  is_system: false,
  member_count: 2,
};

// Convenience actor-permissions set for tests that need a full-power
// admin actor (everything except business.delete / business.transfer_ownership
// — those are owner-only and would let admin grant the owner role).
export const ACTOR_ADMIN_PERMS = new Set([
  'business.read',
  'business.update',
  'roles.read',
  'roles.create',
  'roles.update',
  'roles.delete',
  'members.read',
  'members.invite',
  'members.remove',
  'members.update_role',
]);

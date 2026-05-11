import { describe, it, expect } from 'vitest';
import { PERMISSIONS_BY_ROLE, OWNER_SENTINEL, roleHasPermission } from '@/lib/permissions';

// Fixture: verbatim copy of migrations/postgres/000006_rbac_data_model.up.sql
// lines 70–101 (the JSONB `permissions` column for each system role).
// If you edit the SQL seed, edit this fixture in lockstep.
const SQL_SEED_ADMIN = [
  'business.read',
  'business.update',
  'members.read',
  'members.invite',
  'members.remove',
  'members.update_role',
  'roles.read',
  'roles.create',
  'roles.update',
  'roles.delete',
  'integrations.read',
  'integrations.connect',
  'integrations.disconnect',
  'content.read',
  'content.create',
  'content.update',
  'content.delete',
  'billing.read',
];
const SQL_SEED_EDITOR = [
  'business.read',
  'members.read',
  'roles.read',
  'integrations.read',
  'integrations.connect',
  'integrations.disconnect',
  'content.read',
  'content.create',
  'content.update',
  'content.delete',
];
const SQL_SEED_VIEWER = [
  'business.read',
  'members.read',
  'roles.read',
  'integrations.read',
  'content.read',
  'billing.read',
];

describe('PERMISSIONS_BY_ROLE', () => {
  it('owner uses the * sentinel (no enumeration)', () => {
    expect(PERMISSIONS_BY_ROLE.owner).toEqual([OWNER_SENTINEL]);
  });

  it('admin matches the SQL seed exactly (drift detector — see migrations/postgres/000006)', () => {
    expect([...PERMISSIONS_BY_ROLE.admin].sort()).toEqual([...SQL_SEED_ADMIN].sort());
  });

  it('editor matches the SQL seed exactly (drift detector)', () => {
    expect([...PERMISSIONS_BY_ROLE.editor].sort()).toEqual([...SQL_SEED_EDITOR].sort());
  });

  it('viewer matches the SQL seed exactly (drift detector)', () => {
    expect([...PERMISSIONS_BY_ROLE.viewer].sort()).toEqual([...SQL_SEED_VIEWER].sort());
  });

  it('admin has 18 permissions', () => {
    expect(PERMISSIONS_BY_ROLE.admin).toHaveLength(18);
  });

  it('editor has 10 permissions', () => {
    expect(PERMISSIONS_BY_ROLE.editor).toHaveLength(10);
  });

  it('viewer has 6 permissions', () => {
    expect(PERMISSIONS_BY_ROLE.viewer).toHaveLength(6);
  });
});

describe('roleHasPermission', () => {
  it('owner allows any string via sentinel', () => {
    expect(roleHasPermission('owner', 'business.delete')).toBe(true);
    expect(roleHasPermission('owner', 'made.up.perm')).toBe(true);
  });

  it('admin allows members.invite, denies business.delete (not in seed)', () => {
    expect(roleHasPermission('admin', 'members.invite')).toBe(true);
    expect(roleHasPermission('admin', 'business.delete')).toBe(false);
  });

  it('viewer denies members.invite, allows business.read', () => {
    expect(roleHasPermission('viewer', 'members.invite')).toBe(false);
    expect(roleHasPermission('viewer', 'business.read')).toBe(true);
  });

  it('unknown role denies everything', () => {
    expect(roleHasPermission('marketing-lead', 'members.read')).toBe(false);
  });

  it('undefined role denies everything', () => {
    expect(roleHasPermission(undefined, 'members.read')).toBe(false);
  });
});

import type { Database } from '../../db.js';
import type { UserRole } from '../../auth/tokens.js';
import type { UsersModel } from './model.js';

interface UserRow extends Record<string, unknown> {
  id: string;
  email: string;
  name: string;
  role: UserRole;
  is_active: boolean;
  created_at: string | Date;
  updated_at: string | Date;
}

function mapUser(row: UserRow): UsersModel['user'] {
  return {
    id: row.id,
    email: row.email,
    name: row.name,
    role: row.role,
    is_active: row.is_active,
    created_at: row.created_at instanceof Date ? row.created_at.toISOString() : row.created_at,
    updated_at: row.updated_at instanceof Date ? row.updated_at.toISOString() : row.updated_at,
  };
}

interface UserListRow extends UserRow {
  total_count: string | number;
}

/**
 * Decoupled User management business logic.
 */
export class UserService {
  constructor(private readonly db: Database) {}

  async list(limit: number, offset: number): Promise<UsersModel['listResponse']> {
    // Single query using window COUNT(*) OVER() — avoids separate COUNT roundtrip and prevents N+1
    const result = await this.db.query<UserListRow>(
      `SELECT id, email, name, role, is_active, created_at, updated_at, COUNT(*) OVER() AS total_count
       FROM core.users
       ORDER BY created_at DESC
       LIMIT $1 OFFSET $2`,
      [limit, offset]
    );
    const total = result.rows.length > 0 ? Number(result.rows[0]!.total_count) : 0;
    return {
      users: result.rows.map(mapUser),
      total,
      limit,
      offset,
    };
  }

  async getById(id: string): Promise<UsersModel['user'] | null> {
    const result = await this.db.query<UserRow>(
      'SELECT id, email, name, role, is_active, created_at, updated_at FROM core.users WHERE id = $1',
      [id]
    );
    const row = result.rows[0];
    return row ? mapUser(row) : null;
  }

  async updateName(id: string, name?: string): Promise<UsersModel['user'] | null> {
    const result = await this.db.query<UserRow>(
      'UPDATE core.users SET name = COALESCE($1, name) WHERE id = $2 RETURNING id, email, name, role, is_active, created_at, updated_at',
      [name?.trim() ?? null, id]
    );
    const row = result.rows[0];
    return row ? mapUser(row) : null;
  }

  async remove(id: string): Promise<boolean> {
    const result = await this.db.query('DELETE FROM core.users WHERE id = $1 RETURNING id', [id]);
    return result.rows.length > 0;
  }
}

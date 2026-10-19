# Mesa de Servicios

Mesa de servicio (helpdesk/ticketing) web, multiusuario, en Go + SQLite + SSR + HTMX.

## Características

- **Autenticación**: login/registro público, sesiones con CSRF (doble-submit cookie), roles (admin/agent/requester)
- **Tickets**: creación, estados (new/assigned/in_progress/waiting/resolved/closed) con botón *Actualizar estado*, prioridades (low/normal/high/urgent), colas, asignación, **edición de asunto/descripción y eliminación** (staff: cualquiera; solicitante: los suyos)
- **Plantillas de ticket**: CRUD en Administración › Plantillas, selector en *Nuevo ticket* que precarga asunto, descripción, prioridad y cola (semillas: Soporte, TI, Ciberseguridad)
- **Búsqueda**: campo de texto (ref/asunto/descripción), filtros por estado/prioridad/cola/asignado/SLA y botones de búsqueda rápida
- **SLA**: políticas por prioridad, vencimientos de respuesta y resolución, detección de incumplimiento, avisos automáticos
- **Comunicación**: comentarios públicos e internos, adjuntos con límite configurable
- **Panel**: dashboard con KPIs (abiertos, sin asignar, SLA incumplido, resueltos/creados hoy, cumplimiento SLA, tiempos medios), tabla de actividad 7 días, carga por agente
- **Administración**: gestión de usuarios/colas/plantillas/SLA, auditoría completa
- **Auditoría por ticket**: cada ticket genera `data/tickets/<id>/snapshot.json` (ticket + comentarios + adjuntos + eventos) en cada mutación
- **Copias de seguridad**: export/import de todos los tickets como ZIP (CLI y panel admin), con preservación de IDs y resolución de usuarios/colas
- **Notificaciones**: SMTP con modo dry-run (se registran en logs si no se configura)
- **Multiusuario**: permisos por rol
- **Despliegue**: binario único, SQLite embebido, sin build frontend, fácil en un VPS

## Requisitos

- Go 1.25+ (para desarrollo) o binario precompilado
- SQLite (embebido vía modernc.org/sqlite, sin CGO)

## Instalación (rápida)

### Opción 1: Compilar

```bash
git clone https://github.com/YousefProject/mesa.git
cd mesa
go build -o mesa
./mesa -version
```

### Opción 2: Usar release (GoReleaser)

Los releases incluyen binarios para Linux/macOS/Windows.

## Arranque

Por defecto escucha en `:8080`, guarda datos en `./data/`.

```bash
# Primer arranque: crea administrador por defecto (admin / admin123)
./mesa
```

Salida ejemplo:
```
Administrador creado (credenciales por defecto):
  usuario:    admin
  contraseña: admin123
Cámbiala cuanto antes en Mi cuenta (/me).
```

La primera vez que inicies sesión con esas credenciales, ve a **Mi cuenta** (`/me`) para cambiar el usuario y la contraseña.

### Crear administrador explícito

```bash
./mesa -create-admin -admin-email admin@mesa.local -admin-password CAMBIAR_ESTA_PASSWORD
```

### Flags

- `-version` → muestra versión
- `-create-admin` → crea/actualiza administrador y sale
- `-admin-email` / `-admin-name` / `-admin-password`
- `-addr` → dirección (ej. `:8080` o `127.0.0.1:8080`)
- `-build-snapshots` → regenera `data/tickets/<id>/snapshot.json` para todos los tickets y sale
- `-export-backup <fichero.zip>` → exporta todos los tickets (snapshot + adjuntos) a un ZIP y sale
- `-import-backup <fichero.zip>` → importa los tickets de un ZIP y sale
- `-overwrite` → con `-import-backup`: reemplaza los tickets existentes con el mismo ID (por defecto se omiten)

## Copias de seguridad

El formato es un ZIP autocontenido basado en los snapshots:

```
manifest.json                  # versión del formato, fecha, recuento
tickets/<id>/snapshot.json     # estado completo del ticket
tickets/<id>/attachments/…     # adjuntos
```

Exportar (equivalente CLI y web):

```bash
./mesa -export-backup mesa-backup.zip
# o desde el panel: Administración → Copia → Descargar copia
```

Importar:

```bash
./mesa -import-backup mesa-backup.zip            # omite tickets ya existentes
./mesa -import-backup mesa-backup.zip -overwrite # reemplaza los existentes
# o desde el panel: Administración → Copia → Importar copia
```

Al importar:

- Los **IDs de ticket se conservan** (se adelanta la secuencia AUTOINCREMENT
  para que los nuevos no colisionen); comentarios, adjuntos y auditoría se
  reinsertan con IDs nuevos y se re-mapean entre sí.
- Los **usuarios se resuelven** por correo y luego por ID; si no existen, se
  crea un marcador **inactivo** (no es una cuenta accesible) para no perder
  las referencias.
- Las **colas se resuelven** por ID y luego por nombre; si no existen, se crean.
- Los ficheros de adjuntos se restauran en `data/tickets/<id>/attachments/`
  y cada ticket importado regenera su `snapshot.json`.
- Cada export/import queda registrado en la auditoría (`backup.export`,
  `backup.import`).

## Estructura de datos

Cada componente vive en su propia subcarpeta para poder aislarlo en
migraciones y auditarlo por separado:

```
data/
├── db/
│   └── servicedesk.db        # cuentas, tickets, colas, auditoría (SQLite)
├── tickets/
│   └── <id>/
│       ├── snapshot.json     # estado completo del ticket (JSON auditable)
│       └── attachments/      # adjuntos de ese ticket
└── logs/
```

La **base de datos es la fuente de verdad**; el `snapshot.json` es un espejo
atómico (escritura temp+rename) que se regenera en cada mutación (crear,
comentar, adjuntar, cambiar estado/prioridad/asignado/cola). Sirve para
auditar sin tocar la BD, respaldar por ticket y migrar a otra base de datos.
La capa de persistencia está abstraída tras la interface `store.Repo`
(`internal/store/repo.go`): migrar a Postgres u otra BD es implementar esa
interface sin tocar los handlers.

La estructura plana antigua (`data/servicedesk.db`, `data/attachments/<id>/`)
se migra automáticamente al primer arranque.

## Configuración

La app arranca solo con `./mesa`: toda la configuración (escucha, URL
pública, SMTP, límites, auto-registro, proxy) se edita desde la consola web
en **Administración → Ajustes**, se guarda en la base de datos y se aplica
al instante, sin reiniciar. Cambiar el puerto reabre el escuchador en
caliente.

Las variables de entorno `SD_*` solo aportan los **valores iniciales** antes
de la primera vez que guardas en la consola; `SD_DATA_DIR` es la única que
se lee al arrancar (dónde viven los datos).

| Variable (inicial) | Defecto | Descripción |
|---|---|---|
| `SD_ADDR` | `:8080` | Dirección de escucha |
| `SD_BASE_URL` | `http://localhost:8080` | URL pública (para enlaces de email) |
| `SD_DATA_DIR` | `./data` | Ruta raíz: `db/`, `tickets/`, `logs/` |
| `SD_ALLOW_SIGNUP` | `true` | Permitir registro público |
| `SD_SESSION_TTL` | `168h` | Duración de sesión (7 días) |
| `SD_MAX_UPLOAD_MB` | `10` | Tamaño máximo por subida |
| `SD_COOKIE_SECURE` | `false` | Forzar cookies seguras (HTTPS) |
| `SD_TRUST_PROXY` | `false` | Confiar en `X-Forwarded-For` (detrás de proxy) |
| `SD_SMTP_HOST` | `""` | Host SMTP (vacío = dry-run) |
| `SD_SMTP_PORT` | `587` | Puerto SMTP |
| `SD_SMTP_USER` | `""` | Usuario SMTP |
| `SD_SMTP_PASS` | `""` | Contraseña SMTP |
| `SD_SMTP_FROM` | `mesa@localhost` | From |
| `SD_SMTP_STARTTLS` | `true` | Exigir STARTTLS |
| `SD_ADMIN_EMAIL` | `""` | Admin para bootstrap |
| `SD_ADMIN_PASSWORD` | `""` | Password para bootstrap |

## Uso rápido

1. Ejecuta `./mesa` y accede a `http://localhost:8080`
2. Inicia sesión con `admin` + `admin123` (cámbialas después en `/me`)
3. Ve a `Administración > Usuarios` para crear agentes
4. Ve a `Administración > Colas` para crear colas
5. Crea tus plantillas de ticket en `Administración > Plantillas` (hay 3 de ejemplo)
6. Configura correo y límites en `Administración > Ajustes`
7. Los solicitantes pueden registrarse (`/signup`) o se les crea como requester
8. El dashboard muestra SLA incumplidos y carga por agente

## Sistema de SLA

Las políticas por defecto:
- `urgent` → respuesta 15m / resolución 240m
- `high` → 60m / 480m
- `normal` → 240m / 1440m
- `low` → 480m / 2880m

Cuando se incumple SLA, se marca `sla_breached`, se audita (`sla.breached`) y se notifica al asignado (o a todo el staff si no hay asignado).

## Notas de despliegue

- **Detrás de reverse proxy** (nginx/caddy): activa *Confiar en X-Forwarded-For* en `Administración > Ajustes` si quieres que las IPs sean correctas.
- **HTTPS**: activa *cookies seguras* en `Administración > Ajustes`.
- **Puerto ocupado** (`bind: address already in use`): cierra la instancia anterior (`pkill -x mesa`) o cambia el puerto desde los ajustes.
- **Backups**: copia `data/` entero (o al menos `data/db/` + `data/tickets/`). El WAL (`servicedesk.db-wal/shm`) también debe incluirse o cerrar la app antes de copiar.
- **Auditoría forense**: `data/tickets/<id>/snapshot.json` da el estado completo de cualquier ticket en un momento dado, sin depender de la BD.

## Licencia

MIT

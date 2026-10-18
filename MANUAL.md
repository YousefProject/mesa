# Manual de uso — Mesa de Servicios

Guía completa de uso de la aplicación de mesa de servicios (helpdesk/ticketing).
Para instalación y desarrollo, ver también [README.md](README.md).

---

## Índice

1. [Qué es y para qué sirve](#1-qué-es-y-para-qué-sirve)
2. [Conceptos básicos](#2-conceptos-básicos)
3. [Acceso y cuentas](#3-acceso-y-cuentas)
4. [El panel (dashboard)](#4-el-panel-dashboard)
5. [Gestión de tickets](#5-gestión-de-tickets)
6. [Búsqueda y filtros](#6-búsqueda-y-filtros)
7. [Comentarios y adjuntos](#7-comentarios-y-adjuntos)
8. [SLA (acuerdos de nivel de servicio)](#8-sla-acuerdos-de-nivel-de-servicio)
9. [Administración](#9-administración)
10. [Copias de seguridad](#10-copias-de-seguridad)
11. [Auditoría y snapshots por ticket](#11-auditoría-y-snapshots-por-ticket)
12. [Notificaciones por correo](#12-notificaciones-por-correo)
13. [Estructura de datos](#13-estructura-de-datos)
14. [Configuración (variables de entorno)](#14-configuración-variables-de-entorno)
15. [Solución de problemas](#15-solución-de-problemas)

---

## 1. Qué es y para qué sirve

Mesa de Servicios es una aplicación web multiusuario para gestionar peticiones
de soporte (tickets): los usuarios reportan incidencias o peticiones, el staff
las clasifica, asigna, responde y da por resueltas, controlando los tiempos de
respuesta y resolución con SLA.

Características principales:

- Autenticación con sesiones, CSRF y roles (administrador, agente, solicitante).
- Ciclo de vida completo de tickets con estados, prioridades, colas y asignación.
- SLA configurables por prioridad, con detección de incumplimientos y avisos.
- Comentarios públicos e internos, adjuntos con límite de tamaño.
- Panel con KPIs, carga por agentes y actividad de los últimos 7 días.
- Auditoría completa de todas las acciones.
- Copias de seguridad exportables/importables (ZIP basado en snapshots JSON).
- Notificaciones por correo (o modo dry-run si no hay SMTP configurado).
- Despliegue sencillo: binario único, base de datos SQLite embebida.

---

## 2. Conceptos básicos

| Concepto | Descripción |
|---|---|
| **Ticket** | Una incidencia o petición de soporte. Tiene un identificador público tipo `SD-00001`. |
| **Estado** | Fase del ticket: nuevo, asignado, en curso, esperando, resuelto, cerrado. |
| **Prioridad** | Urgencia: baja, normal, alta, urgente. Determina los plazos SLA. |
| **Cola** | Agrupación lógica de tickets (por ejemplo: Soporte, Redes, Facturación). |
| **Asignado** | Agente responsable del ticket. |
| **Solicitante** | Usuario que creó el ticket. |
| **SLA** | Tiempos máximos de primera respuesta y de resolución según la prioridad. |
| **Snapshot** | JSON con el estado completo de un ticket, generado automáticamente en cada cambio. |

### Estados de un ticket

| Estado | Significado |
|---|---|
| `new` | Recién creado, sin asignar ni trabajar. |
| `assigned` | Asignado a un agente, aún sin comenzar. |
| `in_progress` | En tratamiento activo. |
| `waiting` | En espera de información del solicitante u otra causa externa. |
| `resolved` | Resuelto (el SLA se detiene). |
| `closed` | Cerrado definitivamente. |

### Prioridades y SLA por defecto

| Prioridad | 1ª respuesta | Resolución |
|---|---|---|
| Urgente | 15 minutos | 4 horas |
| Alta | 1 hora | 8 horas |
| Normal | 4 horas | 24 horas |
| Baja | 8 horas | 48 horas |

Los administradores pueden ajustar estos valores en **Administración → SLA**.

---

## 3. Acceso y cuentas

### 3.1 Iniciar sesión

1. Abre `http://localhost:8080` (o la URL desplegada).
2. Pulsa **Entrar** e introduce correo y contraseña.
3. Al autenticarte, se crea una sesión con duración de 7 días (configurable).

Si no tienes cuenta y el registro público está activo, usa **Registrarse**
(`/signup`). Las cuentas nuevas se crean como *solicitantes*.

### 3.2 Roles

| Rol | Permisos |
|---|---|
| **Administrador** (`admin`) | Todo: gestionar usuarios, colas, SLA, auditoría y copias de seguridad, además de los permisos de agente. |
| **Agente** (`agent`) | Ver todos los tickets, triarlos (estado, prioridad, cola, asignación), responder, ver el panel y la auditoría de tickets. |
| **Solicitante** (`requester`) | Crear tickets, ver **solo los suyos**, comentarlos y subir adjuntos. |

### 3.3 Crear el primer administrador

En el primer arranque se crea una cuenta administradora con credenciales por
defecto **usuario `admin` / contraseña `admin123`**. Se recomienda cambiarlas
cuanto antes desde **Mi cuenta** (`/me`), donde también puedes editar el
nombre y el usuario o correo de acceso. La contraseña debe tener al menos 8
caracteres.

Para crear (o sobrescribir) un administrador de forma explícita:

```bash
./mesa -create-admin -admin-email admin@mesa.local -admin-password TuPassword
```

También se puede hacer con variables de entorno (`SD_ADMIN_EMAIL`,
`SD_ADMIN_PASSWORD`).

### 3.4 Mi perfil (`/me`)

- Cambiar nombre visible.
- Cambiar contraseña (requiere la contraseña actual).

### 3.5 Cerrar sesión

Botón **Salir** en la barra superior. La sesión se invalida en el servidor.

---

## 4. El panel (dashboard)

Disponible para agentes y administradores en `/`. Los solicitantes ven en su
lugar una lista de **sus tickets**.

### 4.1 Indicadores (KPIs)

| KPI | Descripción |
|---|---|
| Tickets abiertos | Todos los que no están resueltos ni cerrados. |
| Sin asignar | Abiertos sin agente responsable (clicable para filtrar). |
| SLA incumplido | Tickets con algún plazo vencido (clicable para filtrar). |
| Resueltos hoy | Resueltos desde medianoche. |
| Creados hoy | Creados desde medianoche. |
| Cumplimiento SLA | % de tickets resueltos en los últimos 30 días sin incumplir (verde ≥ 90%, ámbar < 90%). |
| Tiempo medio resolución | Media entre creación y resolución. |
| Tiempo medio 1ª respuesta | Media entre creación y primera respuesta del staff. |

### 4.2 Actividad de los últimos 7 días

Tabla día a día con los tickets **creados** y **resueltos**.

### 4.3 Tarjetas laterales

- **Colas**: tickets abiertos por cola.
- **Carga por agente**: abiertos, incumplimientos SLA y resueltos en 7 días de cada agente.
- **Prioridades abiertas**: abiertos por prioridad.

### 4.4 Tablas del cuerpo

- **Por estado**: reparto de tickets abiertos por estado.
- **Mis asignados**: los 10 tickets asignados a ti (con estado, prioridad y SLA).
- **Actividad reciente**: últimos 10 tickets actualizados (los incumplidos se marcan en rojo).

---

## 5. Gestión de tickets

### 5.1 Crear un ticket

1. Pulsa **+ Nuevo ticket** (barra superior o desde el panel).
2. Rellena:
   - **Asunto** (obligatorio, mínimo 3 caracteres).
   - **Descripción** (opcional pero recomendada: contexto, pasos para reproducir…).
   - **Prioridad** (por defecto: normal).
   - **Cola** y **asignado** (solo el staff puede asignarlos al crear).
3. Pulsa **Crear ticket**. Se te redirige al detalle y el staff recibe una
   notificación por correo.

### 5.2 Detalle de un ticket (`/tickets/<id>`)

Muestra: asunto, descripción, estado, prioridad, cola, asignado, solicitante,
fechas, barra de progreso SLA, comentarios, adjuntos y el historial de
auditoría del ticket.

### 5.3 Cambiar estado / prioridad / cola / asignación (staff)

En el panel lateral derecho de cada ticket hay formularios con desplegables:

- **Estado**: al pasarlo a `resolved` se registra la fecha de resolución, y a
  `closed` también la de cierre. Si vuelves a un estado abierto
  (new/assigned/in_progress/waiting), ambas fechas se limpian.
- **Prioridad**: recalcula automáticamente los plazos SLA desde la fecha de creación.
- **Asignado**: incluye la opción **"Sin asignar"** y "Yo" (asignarme). Al asignar
  a alguien, esa persona recibe un correo.
- **Cola**: mueve el ticket a otra cola.

Todos los cambios quedan en la auditoría del ticket y regeneran su snapshot.

### 5.4 Flujo recomendado

```
new → assigned → in_progress → (waiting ↔ in_progress) → resolved → closed
```

- `waiting` sirve para pausar el reloj de trabajo cuando se necesita algo
  externo (aunque el SLA por calendario sigue contando; tenlo en cuenta).
- `resolved` detiene el SLA. `closed` es el estado final.

### 5.5 Permisos de visualización

- Staff: ve todos los tickets.
- Solicitante: solo ve los que él creó (el servidor lo aplica, no es solo
  ocultamiento en la interfaz).

---

## 6. Búsqueda y filtros

En `/tickets`, la barra de filtros permite combinar:

| Control | Qué filtra |
|---|---|
| **Campo de búsqueda** + botón **Buscar** | Texto en referencia (`SD-…`), asunto o descripción. |
| **Estado** | Un estado concreto o todos. |
| **Prioridad** | Una prioridad concreta o todas. |
| **Cola** | Una cola concreta o todas. |
| **Asignado** (staff) | Una persona, **Sin asignar**, o todos. |
| **Solo SLA incumplido** | Solo los que han vencido. |
| Botón **Filtrar** | Aplica la combinación seleccionada. |
| Botón **Limpiar** | Vuelve a la vista sin filtros. |

### Botones de búsqueda rápida (chips)

Debajo de los filtros hay atajos de un clic:

- **Todos** · **Asignados a mí** · **Sin asignar** · **SLA incumplido**
- **Nuevos** · **Abiertos** · **Pendientes** · **Resueltos** · **Cerrados**

Los chips se pueden combinar con el resto de parámetros editando la URL
(p. ej. `/tickets?q=impresora&status=new`).

---

## 7. Comentarios y adjuntos

### 7.1 Comentarios

- **Públicos**: visibles para el solicitante y el staff.
- **Internos** (solo staff): visibles únicamente para agentes y
  administradores. Marca la casilla *"interno"*.

La primera respuesta del staff a un ticket se registra y detiene el plazo de
primera respuesta del SLA.

### 7.2 Adjuntos

1. En el ticket, usa el formulario **Adjuntar archivos** (formato multipart).
2. Puedes añadir un comentario asociado al subir.
3. Se admiten varios archivos a la vez; el límite por archivo es duro
   (25 MB) y el total por petición es `SD_MAX_UPLOAD_MB` (10 MB por defecto).
4. Los archivos se guardan en `data/tickets/<id>/attachments/` y se descargan
   desde el enlace con el nombre original.

Los nombres de archivo se sanean (sin rutas, caracteres peligrosos reemplazados).

---

## 8. SLA (acuerdos de nivel de servicio)

### 8.1 Cómo funcionan

Al crear un ticket se calculan dos fechas límite según la prioridad:

- **Primera respuesta** (`due_response_at`): plazo para la 1ª respuesta del staff.
- **Resolución** (`due_resolution_at`): plazo para resolver el ticket.

En el detalle del ticket verás barras de progreso y etiquetas de estado:

| Etiqueta | Significado |
|---|---|
| En plazo | Todavía dentro del tiempo. |
| Por vencir | Se ha superado el 75% del plazo (umbral configurable en el código). |
| Incumplido | Plazo vencido sin cumplir. |

### 8.2 Incumplimientos

Cuando un plazo vence:

1. El ticket se marca `sla_breached` (aparece en rojo y en el filtro "SLA incumplido").
2. Se registra un evento de auditoría `sla.breached`.
3. Un trabajador en segundo plano envía un correo al agente asignado (o a todo
   el staff si no hay asignado). Solo se notifica una vez por ticket.

### 8.3 Configurar políticas (admin)

En **Administración → SLA** puedes editar los minutos de respuesta y
resolución de cada prioridad. Los cambios afectan a los tickets **nuevos**;
los existentes mantienen sus fechas calculadas (salvo que cambies su
prioridad, que recalcula desde la fecha de creación).

---

## 9. Administración

Sección exclusiva de administradores, con pestañas: **Usuarios · Colas · SLA ·
Auditoría · Copia**.

### 9.1 Usuarios (`/admin/users`)

- **Crear usuario**: correo, nombre, rol y contraseña inicial. El usuario
  nace activo.
- **Editar**: cambiar rol (administrador/agente/solicitante) y activar/desactivar.
- **Desactivar** en lugar de borrar: el usuario no podrá iniciar sesión, pero
  su historial (tickets, comentarios, auditoría) se conserva.

### 9.2 Colas (`/admin/queues`)

- Crear colas (nombre único + descripción).
- Cada cola muestra cuántos tickets abiertos tiene.
- Los tickets se mueven de cola desde su detalle (staff).

### 9.3 SLA (`/admin/sla`)

- Tabla editable con los minutos de respuesta y resolución por prioridad.

### 9.4 Auditoría (`/admin/audit`)

Registro cronológico de **todas** las acciones del sistema (quién, qué, cuándo
y detalles), por ejemplo:

- `ticket.create`, `ticket.status`, `ticket.priority`, `ticket.assign`, `ticket.queue`
- `comment.create`, `attachment.create`
- `user.create`, `user.role`, `user.active`
- `queue.create`, `sla.update`
- `sla.breached`
- `backup.export`, `backup.import`
- `session.login`, `session.logout`

La auditoría también es consultable por ticket en su página de detalle.

### 9.5 Copia de seguridad (`/admin/backup`)

Ver sección 10.

---

## 10. Copias de seguridad

Sistema de exportación/importación basado en los snapshots JSON de cada
ticket. El ZIP es autocontenido:

```
manifest.json                  # versión del formato, fecha, recuento
tickets/<id>/snapshot.json     # estado completo del ticket
tickets/<id>/attachments/…     # adjuntos
```

### 10.1 Exportar

- **Panel**: Administración → Copia → **Descargar copia (.zip)**.
- **CLI**:

```bash
./mesa -export-backup mesa-backup.zip
```

Antes de empaquetar se regeneran todos los snapshots para que el backup
refleje el estado actual. La operación queda registrada en la auditoría.

### 10.2 Importar

- **Panel**: Administración → Copia → selecciona el ZIP, opcionalmente marca
  *Reemplazar tickets existentes con el mismo ID* y pulsa **Importar copia**.
- **CLI**:

```bash
./mesa -import-backup mesa-backup.zip            # omite tickets ya existentes
./mesa -import-backup mesa-backup.zip -overwrite # reemplaza los existentes
```

### 10.3 Qué hace exactamente la importación

- **IDs de ticket preservados**: se mantiene la referencia (`SD-00001`) y la
  carpeta `data/tickets/<id>/`. La secuencia AUTOINCREMENT se adelanta para
  que los tickets nuevos no colisionen.
- **Comentarios, adjuntos y auditoría** se reinsertan con IDs nuevos
  (re-mapeados entre sí: un adjunto conserva su comentario de origen).
- **Usuarios**: se resuelven primero por correo, luego por ID. Si no existen,
  se crea un marcador **inactivo** con hash inutilizable (no es una cuenta
  accesible) para no perder las referencias históricas.
- **Colas**: se resuelven por ID y luego por nombre; si no existen, se crean.
- **Adjuntos**: los ficheros se restauran en `data/tickets/<id>/attachments/`.
- Cada ticket importado regenera su `snapshot.json`.
- Los avisos (usuarios creados, adjuntos ausentes en el ZIP, etc.) se
  registran en los logs y se resumen al final.

### 10.4 Escenario típico de restauración

```bash
# 1. Exportar desde el sistema sano
./mesa -export-backup respaldo.zip

# 2. (Desastre) borrar o dañar data/ … 

# 3. Arrancar de cero y restaurar
./mesa -create-admin -admin-email admin@local -admin-password ***
./mesa -import-backup respaldo.zip
./mesa -build-snapshots   # opcional: regenerar todos los snapshots
```

---

## 11. Auditoría y snapshots por ticket

### 11.1 Auditoría

Cada acción relevante genera un evento inmutable (actor, acción, entidad,
detalles, fecha). Dos vistas:

- Global: **Administración → Auditoría**.
- Por ticket: panel de historial en el detalle de cada ticket.

### 11.2 Snapshots JSON

En **cada mutación** de un ticket (crear, comentar, adjuntar, cambiar estado,
prioridad, asignado o cola) se regenera de forma atómica
(escritura temporal + renombrado) el fichero:

```
data/tickets/<id>/snapshot.json
```

Contenido (formato portable con tags JSON):

```json
{
  "snapshot_version": 1,
  "ticket":     { "id": 1, "ref": "SD-00001", "subject": "…", "status": "new", … },
  "comments":   [ … ],
  "attachments":[ … ],
  "events":     [ … ],
  "saved_at":   "2026-10-18T21:39:44Z"
}
```

Usos:

- **Auditoría forense**: estado completo de cualquier ticket sin tocar la BD.
- **Base del sistema de copias de seguridad** (sección 10).
- **Migración** a otra base de datos.

Para regenerar todos los snapshots de golpe (p. ej. tras una migración grande):

```bash
./mesa -build-snapshots
```

La base de datos sigue siendo la **fuente de verdad**; el snapshot es un
espejo auditable.

---

## 12. Notificaciones por correo

Si `SD_SMTP_HOST` está configurado, la aplicación envía correos para:

| Evento | Destinatarios |
|---|---|
| Ticket creado | Todo el staff y el solicitante. |
| Comentario público | Solicitante y, si hay asignado, el asignado; si no hay asignado, todo el staff (nunca al autor del comentario). |
| Ticket asignado | El agente asignado. |
| SLA incumplido | El asignado, o todo el staff si no hay asignado (una sola vez). |

Si **no** hay SMTP configurado (modo dry-run, por defecto), los correos no se
envían: se registran en la salida con el asunto y el cuerpo, lo que permite
probar el flujo completo sin servidor de correo.

Asunto típico: `[SD-00001] Nuevo ticket: asunto del ticket`.

---

## 13. Estructura de datos

Cada componente vive en su subcarpeta, para poder aislarlo en migraciones y
auditarlo por separado:

```
data/
├── db/
│   └── servicedesk.db        # cuentas, tickets, colas, auditoría (SQLite)
├── tickets/
│   └── <id>/
│       ├── snapshot.json     # estado completo del ticket (JSON)
│       └── attachments/      # adjuntos de ese ticket
└── logs/
```

La estructura plana antigua (`data/servicedesk.db`, `data/attachments/<id>/`)
se migra automáticamente al primer arranque.

**Backups**: copia `data/` entero (o al menos `data/db/` y `data/tickets/`).
Cierra la aplicación antes de copiar los ficheros de la BD para incluir el
WAL de forma consistente, o usa el sistema de copias de seguridad de la
sección 10, que no requiere parar el servicio.

---

## 14. Configuración (variables de entorno)

| Variable | Defecto | Descripción |
|---|---|---|
| `SD_ADDR` | `:8080` | Dirección de escucha. |
| `SD_BASE_URL` | `http://localhost:8080` | URL pública (para enlaces de correo). |
| `SD_DATA_DIR` | `./data` | Ruta raíz: `db/`, `tickets/`, `logs/`. |
| `SD_ALLOW_SIGNUP` | `true` | Registro público en `/signup`. |
| `SD_SESSION_TTL` | `168h` | Duración de la sesión (7 días). |
| `SD_COOKIE_SECURE` | `false` | Forzar cookies seguras (usar con HTTPS). |
| `SD_TRUST_PROXY` | `false` | Confiar en `X-Forwarded-For` (detrás de proxy). |
| `SD_MAX_UPLOAD_MB` | `10` | Tamaño máximo total por subida. |
| `SD_SMTP_HOST` | vacío | Host SMTP (vacío = dry-run). |
| `SD_SMTP_PORT` | `587` | Puerto SMTP. |
| `SD_SMTP_USER` | vacío | Usuario SMTP. |
| `SD_SMTP_PASS` | vacío | Contraseña SMTP. |
| `SD_SMTP_FROM` | `servicedesk@localhost` | Remitente. |
| `SD_SMTP_STARTTLS` | `true` | Usar STARTTLS. |
| `SD_ADMIN_EMAIL` | vacío | Admin para bootstrap automático. |
| `SD_ADMIN_PASSWORD` | vacío | Contraseña del admin de bootstrap. |

### Flags de CLI

| Flag | Descripción |
|---|---|
| `-version` | Muestra versión y sale. |
| `-addr` | Sobreescribe la dirección de escucha. |
| `-create-admin` | Crea/actualiza el administrador y sale. |
| `-admin-email` / `-admin-name` / `-admin-password` | Datos del admin. |
| `-build-snapshots` | Regenera todos los `snapshot.json` y sale. |
| `-export-backup <fichero.zip>` | Exporta todos los tickets a un ZIP y sale. |
| `-import-backup <fichero.zip>` | Importa tickets de un ZIP y sale. |
| `-overwrite` | Con `-import-backup`: reemplaza tickets existentes con el mismo ID. |

### Despliegue detrás de reverse proxy

```nginx
# Ejemplo nginx; activar SD_TRUST_PROXY=true para IPs reales
location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
}
```

Con HTTPS, activa además `SD_COOKIE_SECURE=true`.

---

## 15. Solución de problemas

| Síntoma | Causa probable | Solución |
|---|---|---|
| `403 token CSRF inválido` al enviar formularios | Token de sesión caducado o formulario con HTML cacheado. | Recarga la página y reintenta. |
| No llegan correos | SMTP sin configurar o incorrecto. | Revisa `SD_SMTP_*`; en modo dry-run los correos salen por log. |
| No puedo ver un ticket (404/403) | Eres solicitante y el ticket es de otro usuario. | Solo el staff ve todos los tickets. |
| El filtro "Sin asignar" no funciona | — (corregido; usa `assignee=none`). | Actualiza a la última versión. |
| La subida de adjuntos da error | Archivo o suma total demasiado grande. | Comprueba `SD_MAX_UPLOAD_MB` (25 MB máx. por archivo). |
| `SLA incumplido` aparece en tickets vivos | El plazo venció mientras estaba abierto. | Resolver/cerrar el ticket o ajustar políticas en **Administración → SLA**. |
| El admin no recuerda la contraseña | Se generó aleatoriamente en el primer arranque. | Recrear con `-create-admin -admin-password …`. |
| Importación de backup omite tickets | Ya existen con ese ID. | Usa `-overwrite` (CLI) o marca *Reemplazar* (panel). |
| `sqlite: database is locked` | Otra escritura simultánea. | Normal con WAL; si persiste, revisa discos de red o antivirus. |
| Los KPIs del panel muestran 0 | No hay tickets o datos aún. | Crea tickets de prueba. |

### Comprobación de salud

```bash
curl http://localhost:8080/healthz   # → "ok"
```

Útil para sondeos de supervisión (systemd, Docker, etc.).

---

## Rutas HTTP (resumen)

| Método | Ruta | Acceso |
|---|---|---|
| GET | `/` | Panel (staff) o mis tickets (solicitante) |
| GET/POST | `/login`, `/signup`, `POST /logout` | Público / autenticado |
| GET | `/tickets` | Autenticado |
| GET/POST | `/tickets/new`, `/tickets` (POST crea) | Autenticado |
| GET | `/tickets/{id}` | Según permisos |
| POST | `/tickets/{id}/comments` | Autor del ticket o staff |
| POST | `/tickets/{id}/status\|priority\|assign\|queue` | Staff |
| POST | `/tickets/{id}/attachments` | Autor o staff |
| GET | `/attachments/{id}` | Según permisos del ticket |
| GET | `/dashboard` | Staff |
| GET/POST | `/me`, `POST /me/password` | Autenticado |
| GET/POST | `/admin/users`, `/admin/queues`, `/admin/sla` | Admin |
| GET | `/admin/audit` | Admin |
| GET | `/admin/backup`, `/admin/backup/export`, `POST /admin/backup/import` | Admin |
| GET | `/healthz` | Público |

---

*Manual correspondiente a la versión con copias de seguridad por snapshot,
búsqueda con chips, dashboard ampliado y estructura de datos aislada por
componente.*

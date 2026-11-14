# Mejoras futuras

## HTTPS para instalaciones locales

Seguimiento: [Plan assisted HTTPS setup for local installations](https://github.com/Pegasus8/piworker/issues/287).

Contexto: el uso es dentro de una red local, sin exigir dominio ni certificados
públicos. Implementado según [ADR-0011](docs/adr/0011-native-https-with-local-ca.md):
PiWorker sirve HTTPS por defecto en el puerto 8443 con una CA local generada en
el primer arranque; el puerto 8080 redirige a HTTPS (salvo `/api/health`, la
descarga de la CA y los webhooks). Ver la [guía de operación](docs/operations.md#https-and-the-local-ca).

- [x] Mostrar una advertencia clara cuando la interfaz se utilice mediante HTTP:
  la conexión no está cifrada y las credenciales y sesiones pueden ser
  interceptadas, incluso dentro de una red local.
- [x] Ofrecer desde esa advertencia una guía para proteger la conexión con HTTPS
  (página "Trust this device"). Certificados propios con `tls.mode = "custom"`;
  detrás de un proxy HTTPS, `tls.mode = "off"` y `session_secure`.
- [x] Configuración asistida de HTTPS desde PiWorker: CA local y certificado del
  servidor generados al primer arranque. Las instalaciones nuevas arrancan en
  HTTPS (versión previa al primer lanzamiento: ninguna URL publicada cambia en
  silencio); el instalador imprime la dirección HTTPS y la huella de la CA.
- [x] Explicar las advertencias del navegador y los pasos necesarios en cada
  dispositivo cliente (guías por sistema en la página "Trust this device").
- [x] Documentar cómo verificar la huella de la CA desde el equipo donde corre
  PiWorker (consola/journal, `piworkerctl install`, `openssl`) antes de confiar en
  ella, el alcance de esa confianza y cómo retirarla, sin recomendar desactivar la
  validación TLS ni ignorar advertencias. La página también lo pide cuando llega
  por HTTPS, porque antes de confiar en la CA un impostor podría servirla.
- [x] Renovación y caducidad: el certificado del servidor se renueva solo 30 días
  antes de vencer y nunca dura más que la CA; los cambios de dirección o nombre se
  detectan cada 10 minutos; desde 90 días antes de que venza la CA (10 años) el log
  y la página avisan (`piworker -tls-regenerate-ca`). Recuperación de acceso con
  `piworkerctl tls off`, `-tls=off` o `PIWORKER_TLS_MODE=off`.
- [x] Manejar HTTP local con cookies HttpOnly/SameSite=Strict y permitir cookies
  `Secure` mediante `session_secure` para un proxy HTTPS externo. Esta opción no
  activa TLS. Las cabeceras de transporte del proxy no se confían implícitamente.
  Con HTTPS nativo las cookies son siempre `Secure`.
- [x] Issue #287: ya no figura entre los issues abiertos (revisado el 2026-11-11).

La confianza del navegador requiere una acción en los dispositivos cliente o
un certificado emitido por una autoridad que ya reconozcan; el servidor no puede
eliminar por sí solo las advertencias de un certificado autofirmado.

## Variables y secretos

La gestión de variables globales y por flujo, las referencias en expresiones/plantillas y el
selector contextual de nombres están implementados. Queda como ampliación:

- [x] Variables por flujo persistentes, selector de alcance, autocompletado y ayuda
  contextual en la UI. Las referencias globales existentes conservan su significado.
- [x] Incorporar referencias a secretos en credenciales HTTP, con pruebas de que
  los valores no aparecen en payloads, previews ni mensajes de error
  (`{{secret.NAME}}` en cabeceras y en el usuario/consulta de la URL; ver
  [docs/design/mcp.md §3](docs/design/mcp.md)).
- [x] Rechazar al desplegar referencias a secretos inexistentes en los ajustes
  que no funcionan sin ellos (token de Telegram, URL de notificación, DSN de
  base de datos: `node.SecretRequirer`, [ADR-0019](docs/adr/0019-lint-markers-and-change-alerts-define-low.md));
  Lint las informa como `undefined_secret`.
- [x] En los demás ajustes (credenciales opcionales de Email y MQTT, URL y
  cabeceras de HTTP Request) un secreto inexistente ya no pasa en silencio:
  Lint avisa con `unresolved_secret`, el despliegue devuelve el aviso y el
  editor lo muestra ([ADR-0028](docs/adr/0028-warn-about-undefined-optional-secrets-and-list-secret-uses.md)).
- [x] Mostrar qué flujos usan cada secreto: el diálogo de secretos lista
  flujo, nodo, ajuste y conexiones de broker que lo usan, avisa antes de
  borrar uno en uso y muestra los nombres usados pero no definidos (ADR-0028).
- [x] Evaluar cifrado de secretos en reposo: revisado y decidido no cifrar en
  la aplicación (en una Pi la clave estaría en la misma tarjeta); la base de
  datos se crea privada (0600) y pierde el acceso de otros usuarios al
  arrancar; se recomienda cifrado de disco
  ([ADR-0030](docs/adr/0030-no-application-encryption-of-secrets-at-rest.md),
  [revisión](docs/design/secrets-at-rest.md)).
- [x] Exportación/restauración de variables globales, por flujo, conexiones y
  secretos, con los valores de secretos cifrados con frase de paso
  ([ADR-0031](docs/adr/0031-export-and-restore-of-variables-connections-and-secrets.md)).

## Documentación

- [x] Guía de usuario en [docs/guide](docs/guide/README.md): primeros pasos con
  capturas, conceptos, recetas, agentes y resolución de problemas.
- [x] Referencia de nodos generada desde el registro
  (`TestNodeReferenceDocIsCurrent`).
- [ ] Reducir el README a una vista general y mover la referencia de
  configuración, HTTPS y agentes a `docs/`.
- [x] Mostrar el riesgo de cada nodo en el editor: la lista de nodos marca los
  externos y privilegiados y el panel de ajustes explica el nivel.
- [x] Flujos de ejemplo importables desde la pantalla vacía (lista de flujos,
  «Empezar desde un ejemplo»).

- [ ] Validar en hardware real los nodos GPIO Edge, GPIO Write, I2C Read y
  DS18B20 y quitarles la marca de experimentales.

## Domótica Zigbee

Análisis y propuesta por fases en [docs/design/zigbee.md](docs/design/zigbee.md)
(integración vía Zigbee2MQTT, único backend por ahora). Primer hardware: sensor
de temperatura y humedad SONOFF y enchufes SONOFF.

- [x] Fase 0: presupuesto del listado de tipos de nodo para agentes
  ([ADR-0024](docs/adr/0024-node-type-list-without-filters-omits-documentation.md))
  y conexiones MQTT compartidas
  ([ADR-0025](docs/adr/0025-shared-mqtt-connections.md)).
- [x] Conexión de broker con nombre en el editor
  ([ADR-0026](docs/adr/0026-named-broker-connections.md)).
- [x] Fase 1: nodos Zigbee Device, Zigbee Set, Zigbee Get, Timer y Sol, con
  `docker-compose.zigbee.yml` y la [guía](docs/guide/zigbee.md).
- [x] Fase 2: descubrimiento de dispositivos, sugerencias en los ajustes de
  los nodos, página Zigbee con estado del bridge y permit join (solo admin).
- [x] Fase 3: estado de los dispositivos en expresiones (`zigbee("...")`),
  recetas de batería y flujos de ejemplo.
- [ ] Validar con hardware real (SONOFF S60ZBTPF y SNZB-02P, coordinador
  USB) en una Raspberry Pi y en un PC x86_64.
- [x] Los nodos MQTT aceptan una conexión con nombre; guardar una conexión
  reinicia los flujos en marcha que la usan y no se puede borrar una que use
  un flujo en marcha ([ADR-0029](docs/adr/0029-mqtt-nodes-take-named-connections-and-changes-restart-flows.md)).

Lo que falta para publicar está en
[docs/release-checklist.md](docs/release-checklist.md#launch-gap-review-2026-11-11).

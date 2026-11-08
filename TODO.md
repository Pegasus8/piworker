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
- [ ] Cerrar el issue #287 una vez integrado.

La confianza del navegador requiere una acción en los dispositivos cliente o
un certificado emitido por una autoridad que ya reconozcan; el servidor no puede
eliminar por sí solo las advertencias de un certificado autofirmado.

## Variables y secretos

La gestión de variables globales y por flujo, las referencias en expresiones/plantillas y el
selector contextual de nombres están implementados. Queda como ampliación:

- [x] Variables por flujo persistentes, selector de alcance, autocompletado y ayuda
  contextual en la UI. Las referencias globales existentes conservan su significado.
- [ ] Incorporar referencias a secretos en credenciales HTTP, con pruebas de que
  los valores no aparecen en payloads, previews ni mensajes de error.
- [ ] Rechazar referencias a secretos inexistentes al desplegar, en lugar de
  resolverlas a una cadena vacía, y mostrar qué flujos usan cada secreto.
- [ ] Evaluar cifrado de secretos en reposo con gestión y recuperación de claves.
  Actualmente SQLite guarda los valores sin cifrado propio de la aplicación.
- [ ] Diseñar exportación/restauración explícita de variables globales, por flujo y secretos;
  exportar un flujo no incluye los almacenes de la instalación.

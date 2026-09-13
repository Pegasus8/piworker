# Mejoras futuras

## HTTPS para instalaciones locales

Seguimiento: [Plan assisted HTTPS setup for local installations](https://github.com/Pegasus8/piworker/issues/287).

Contexto: el uso inicial será dentro de una red local, sin exigir dominio ni
certificados. El servidor sigue usando HTTP; no hay listener TLS ni asistente de
certificados. Las tareas sin marcar quedan pendientes.

- [x] Mostrar una advertencia clara cuando la interfaz se utilice mediante HTTP:
  la conexión no está cifrada y las credenciales y sesiones pueden ser
  interceptadas, incluso dentro de una red local.
- [ ] Ofrecer desde esa advertencia una guía para proteger la conexión con HTTPS.
  Contemplar certificados configurados por el usuario y un proxy HTTPS.
- [ ] Evaluar una configuración asistida de HTTPS desde PiWorker, incluida la
  generación de un certificado local, sin cambiar silenciosamente el acceso.
- [ ] Explicar antes de activar un certificado autofirmado las advertencias que
  mostrará el navegador y los pasos necesarios en cada dispositivo cliente.
- [ ] Documentar cómo verificar la identidad y la huella del certificado desde
  el equipo donde corre PiWorker antes de confiar en él. Explicar el alcance de
  esa confianza y cómo retirarla; evitar recomendar desactivar la validación TLS
  o ignorar advertencias de certificados desconocidos.
- [ ] Diseñar renovación, caducidad, cambios de dirección del dispositivo y
  recuperación de acceso si la configuración HTTPS falla.
- [x] Manejar HTTP local con cookies HttpOnly/SameSite=Strict y permitir cookies
  `Secure` mediante `session_secure` para un proxy HTTPS externo. Esta opción no
  activa TLS. Las cabeceras de transporte del proxy no se confían implícitamente.

La confianza del navegador requiere una acción en los dispositivos cliente o
un certificado emitido por una autoridad que ya reconozcan; el servidor no puede
eliminar por sí solo las advertencias de un certificado autofirmado.

## Variables y secretos

La gestión de variables globales, las referencias en expresiones/plantillas y el
selector contextual de nombres están implementados. Queda como ampliación:

- [ ] Definir e implementar un alcance de variables por flujo; el campo del modelo
  actual no constituye una implementación de ese alcance.
- [ ] Incorporar referencias a secretos en credenciales HTTP, con pruebas de que
  los valores no aparecen en payloads, previews ni mensajes de error.
- [ ] Rechazar referencias a secretos inexistentes al desplegar, en lugar de
  resolverlas a una cadena vacía, y mostrar qué flujos usan cada secreto.
- [ ] Evaluar cifrado de secretos en reposo con gestión y recuperación de claves.
  Actualmente SQLite guarda los valores sin cifrado propio de la aplicación.
- [ ] Diseñar exportación/restauración explícita de variables globales y secretos;
  exportar un flujo no incluye los almacenes de la instalación.

# NOTIFICATION DELIVERY SERVICE
A backend notification delivery system built in Go that reliably sends notifications through multiple channels
while handling different delivery requirements, priorities, retries and provider failures.


The system will support:
- **SMS** through providers such as Twilio and Africa's Talking
- **Email**
- **Webhooks**


The main goal of the project is to explore the engineering challenges involved in building a reliable asynchronous notification system, including:

- Asynchronous job processing 
- Priority and deadline-based scheduling 
- Worker pools and concurrency 
- Notification SLAs 
- Retries and failure handling 
- Idempotency and duplicate prevention 
- Provider selection and failover 
- Notification expiration 
- Delivery status tracking 
- Observability and system reliability

The service will accept notification requests through an API, assign each notification a unique identity, persists its state, schedule it based on its delivery deadline and use workers to processs and deliver the notification through the appropriate channel provider
### HIGH LEVEL ARCHITECTURE

**Design:** [View Architecture Design](https://excalidraw.com/#room=9cdd6d7ef4618b79cd24,cNnRmLdaNxl3ej-4xWGAOg)

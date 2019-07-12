<template>
  <div
    class="modal fade"
    id="newTaskModal"
    tabindex="-1"
    role="dialog"
    aria-labelledby="modalTitle"
    aria-hidden="true"
  >
    <div class="modal-dialog modal-dialog-centered" role="document">
      <div class="modal-content">
        <div class="modal-header">
          <h5 class="modal-title" id="modalTitle">Create a new task</h5>
          <button type="button" class="close" data-dismiss="modal" aria-label="Close">
            <span aria-hidden="true">&times;</span>
          </button>
        </div>
        <div class="modal-body">
          <!-- SECTION Form -->
          <div class="form m-3">
            <div class="form-group">
              <label for="task-name" class="text-light">Name</label>
              <input
                type="text"
                class="form-control"
                aria-describedby="task-name-description"
                placeholder="Enter a task name"
                id="task-name"
              />
              <small
                id="task-name-description"
                class="form-text text-muted text-left"
              >The name for your new task.</small>
            </div>

            <div class="form-group">
              <label for="default-state" class="text-light">Default state</label>
              <select id="default-state" class="form-control">
                <option>Active</option>
                <option>Inactive</option>
              </select>
            </div>

            <div class="form-group">
              <label for="trigger-slector" class="text-light">Trigger</label>
              <select
                id="trigger-selector"
                class="form-control"
                aria-describedby="trigger-selector-description"
              >
                <option>...</option>
                <!-- TODO Add triggers dynamically -->
              </select>
              <small id="trigger-selector-description" class="form-text text-muted text-left">
                Select one trigger.
              </small>
              <!-- TODO Selection of args -->
            </div>

            <div class="form group">
              <label for="actions-selector" class="text-light">Action/s</label>
              <select
                id="actions-selector"
                class="form-control"
                aria-describedby="actions-selector-description"
              >
                <option>...</option>
                <!-- TODO Add actions dynamically -->
              </select>
              <small id="actions-selector-description" class="form-text text-muted text-left">
                Select one or more actions.
              </small>
              <!-- TODO Selection of args per task -->
              <!-- TODO Add the selected actions into a list -->
            </div>
          </div>
        </div>
        <div class="modal-footer">
          <button type="button" class="btn btn-secondary" data-dismiss="modal">Close</button>
          <button type="button" class="btn btn-primary">Save</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script>
export default {}
</script>

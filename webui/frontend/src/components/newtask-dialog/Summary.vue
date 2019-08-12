<template>
<div class="mt-4 border rounded summary p-2">
  <h2 class="text-bold">Summary</h2>

  <div class="p-3">

    <p class="font-weight-normal text-left">
      Name of the task: <span class="font-weight-bold">{{ taskName }}</span>
    </p>

    <p class="font-weight-normal text-left">
      Default state: <span class="font-weight-bold">{{ taskState }}</span>
    </p>

    <div class="p-1">
      <h5 class="mt-1">Trigger selected</h5>
      <draggable 
        v-model="triggers" 
        @start="drag=true" 
        @end="drag=false"
        class="list-group">
        <!-- NOTE For trigger parsing, is unnecessary do a for loop
        due to there is a only one trigger per task. This was made
        in this way for a future implementation of multiple triggers. -->
        <div
          v-for="userTrigger in triggers" 
          :key="userTrigger.ID + '_' + $uuid.v1()" 
          :title="userTrigger.Description"
          class="list-group-item text-break text-bolder">
          {{ userTrigger.Name }}
          <button 
            type="button" 
            class="close" 
            aria-label="Remove trigger"
            @click="removeTrigger">
            <span aria-hidden="true">&times;</span>
          </button>
        </div>
      </draggable>
    </div>

    <div class="p-1">
      <h5 class="mt-1">Actions selected</h5>
      <draggable 
        v-model="actions" 
        @start="drag=true" 
        @end="drag=false"
        class="list-group">
        <div 
          v-for="(userAction, index) in actions" 
          :key="userAction.ID + '_' + $uuid.v1()" 
          :title="userAction.Description"
          class="list-group-item text-break text-bolder">
          {{ userAction.Name }}
          <button 
            type="button" 
            class="close" 
            aria-label="Remove action"
            @click="removeAction(index)">
            <span aria-hidden="true">&times;</span>
          </button>
        </div>
      </draggable>
      <small class="text-muted">Tip: drag and drop for order the actions</small>
    </div>
    
  </div>

</div>
</template>

<script>
import draggable from 'vuedraggable'
import { mapMutations } from 'vuex' 
export default {
  computed: {
    taskName() {
      return this.$store.getters.taskname
    },
    taskState() {
      return this.$store.getters.taskState
    },
    triggers: {
      get() {
        return this.$store.getters.triggerSelected
      },
      set(newValue) {
        this.$store.commit('setTrigger', newValue)
      }
    },
    actions: {
      get() {
        return this.$store.getters.actionsSelected
      },
      set(newValue) {
        this.$store.commit('setActions', newValue)
      }
    }
  },
  methods: {
    ...mapMutations([
      'setTrigger', 'removeTrigger',
      'setActions', 'removeAction'
    ])
  },
  components: {
    draggable
  }
}
</script>

<style lang="scss" scoped>
li {
  list-style: none;
}
.summary {
  background-color: rgba(221, 221, 221, 0.411);
}
</style>



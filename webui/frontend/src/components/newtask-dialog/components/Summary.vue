<template>
<div class="card">
  <div class="card-header font-weight-bold">
    Summary
  </div>

  <div class="card-body p-3">

    <app-summary-card 
      cardTitle="Name of the task"
      :contentToEvaluate="taskName">
      {{ taskName }}
    </app-summary-card>

    <app-summary-card
      cardTitle="Default state"
      :contentToEvaluate="taskState">
      {{ taskState }}
    </app-summary-card>

    <app-summary-card
      cardTitle="Trigger selected"
      :contentToEvaluate="triggers">
      <draggable 
        v-model="triggers"
        v-if="triggers.length > 0"
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
    </app-summary-card>
    
    <app-summary-card
      cardTitle="Actions selected"
      :contentToEvaluate="actions">
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
    </app-summary-card>
    
  </div>

</div>


</template>

<script>
import draggable from 'vuedraggable'
import { mapMutations } from 'vuex'
import SummaryCard from './SummaryCard.vue'

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
    draggable,
    appSummaryCard: SummaryCard
  },
  filters: {
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



<template>
<b-card class="card bg-dark text-white" id="summary-card">
  <b-card-header class="font-weight-bold">
    Summary
  </b-card-header>

  <b-card-body class="p-3">

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
      :contentToEvaluate="triggers"
      @switchChange="canDragTriggers = $event">
      <draggable
        v-model="triggers"
        v-if="triggers.length > 0"
        :disabled="!canDragTriggers"
        @start="drag=true"
        @end="drag=false"
        class="list-group">
        <!-- NOTE For trigger parsing, is unnecessary do a for loop
        due to there is a only one trigger per task. This was made
        in this way for a future implementation of multiple triggers. -->
        <div
          v-for="(userTrigger, index) in triggers"
          :key="userTrigger.ID + '_' + $uuid.v1()"
          class="list-group-item">
          <div class="d-flex">
            <div class="flex-grow-1 text-break text-bolder text-dark">
              {{ userTrigger.name }}
              <router-link
                v-b-popover.hover.top="userTrigger.description"
                title="Trigger Description"
                tag="span"
                class="icon-info mx-1"
                to=""
              />
            </div>
            <button
              type="button"
              class="close"
              aria-label="Remove trigger"
              @click="removeTrigger(index)">
              <span aria-hidden="true">&times;</span>
            </button>
          </div>
          <b-row class="border rounded m-2 p-1">

            <b-col
              cols="10"
              md="6"
              class="mx-auto my-2"
              v-for="arg in userTrigger.args" :key="arg.ID + '_' + $uuid.v1()">
              <b-card no-body bg-variant="light" :title="arg.description">
                <b-card-header class="h5 p-1 text-wrap text-dark">
                  {{ arg.name }}
                </b-card-header>
                <b-card-body class="text-wrap">
                  <!-- Don't use the b-form-input -->
                  <input
                    :type="arg.contentType"
                    class="form-control"
                    placeholder="Content"
                    aria-label="Argument content"
                    v-model.lazy="arg.content">
                </b-card-body>
              </b-card>
            </b-col>

          </b-row>
        </div>
      </draggable>
    </app-summary-card>

    <app-summary-card
      cardTitle="Actions selected"
      :contentToEvaluate="actions"
      @switchChange="canDragActions = $event">
      <draggable
        v-model="actions"
        :disabled="!canDragActions"
        @start="drag=true"
        @end="drag=false"
        class="list-group">
        <div
          v-for="(userAction, index) in actions"
          :key="userAction.ID + '_' + $uuid.v1()"
          class="list-group-item">
          <div v-if="userAction.chained" class="bg-primary rounded mb-2">
            <div style="opacity: 0.7;" class="icon-box-add my-1"></div>
            <b-row class="justify-content-center">
              <b-col cols='10' md='8' lg='6'>
                <b-form-select size='sm' class="my-1" v-model='userAction.argumentToReplaceByCR' :options='userAction.args | prepareChainedArgsSelect' />
              </b-col>
            </b-row>
          </div>
          <div class="d-flex">
            <div class="flex-grow-1 text-break text-bolder text-dark">
              {{ userAction.name }}
              <router-link
                v-b-popover.hover.top="userAction.description"
                title="Action Description"
                tag="span"
                class="icon-info mx-1"
                to=""
              />
            </div>
            <button
              type="button"
              class="close"
              aria-label="Remove action"
              @click="removeAction(index)">
              <span aria-hidden="true">&times;</span>
            </button>
          </div>
          <b-row class="border rounded m-2 p-1 row">

            <b-col
              cols="10"
              md="6"
              class="mx-auto my-2"
              v-for="arg in userAction.args" :key="arg.ID + '_' + $uuid.v1()"
            >
              <b-card no-body bg-variant="light" :title="arg.description">
                <b-card-header class="h5 p-1 text-wrap text-dark">
                  {{ arg.name }}
                </b-card-header>
                <b-card-body class="text-wrap">
                  <!-- Don't use b-form-input -->
                  <input
                    :type="arg.contentType | filterIncompatibleTypes" 
                    class="form-control"
                    placeholder="Content"
                    aria-label="Argument content"
                    v-model.lazy="arg.content">
                </b-card-body>
              </b-card>
            </b-col>

          </b-row>
          <b-form-checkbox class="text-dark" v-model="userAction.chained" switch>
            <span class="small">Chained
              <router-link
                :id="'chained-action-info' + index"
                tag="span"
                class="icon-info mx-1 small"
                to=""
              />
              <b-popover :target="'chained-action-info' + index" triggers="hover" placement="righttop">
                <template v-slot:title>
                  Chained Actions
                </template>
                <p>A chained action is one that uses the result returned by the previous action.</p>
                <p><b>ON</b>: uses the result of the previous action, overwriting any of the arguments entered manually by the user.</p>
                <p><b>OFF</b>: use only the arguments provided by the user.</p>
              </b-popover>
            </span>
          </b-form-checkbox>
        </div>
      </draggable>
      <small class="text-muted">Tip: drag and drop for order the actions</small>
    </app-summary-card>

  </b-card-body>

</b-card>

</template>

<script>
import draggable from 'vuedraggable'
import { mapMutations } from 'vuex'
import SummaryCard from './SummaryCard.vue'

export default {
  data () {
    return {
      canDragActions: true,
      canDragTriggers: true
    }
  },
  computed: {
    taskName () {
      return this.$store.getters['newTask/taskname']
    },
    taskState () {
      return this.$store.getters['newTask/taskState']
    },
    triggers: {
      get () {
        return this.$store.getters['newTask/triggerSelected']
      },
      set (newValue) {
        this.$store.commit('newTask/setTrigger', newValue)
      }
    },
    actions: {
      get () {
        return this.$store.getters['newTask/actionsSelected']
      },
      set (newValue) {
        this.$store.commit('newTask/setActions', newValue)
      }
    }
  },
  methods: {
    ...mapMutations('newTask', [
      'setTrigger', 'removeTrigger',
      'setActions', 'removeAction'
    ])
  },
  components: {
    draggable,
    appSummaryCard: SummaryCard
  },
  filters: {
    prepareChainedArgsSelect (args) {
      let argsArray = []
      args.forEach((arg) => {
        argsArray.push({ text: arg.name, value: arg.ID })
      })
      return argsArray
    },
    filterIncompatibleTypes (argType) {
      // If the type is not supported by default (like JSON, path, etc), replace it with a compatible one.
      // NOTE This is temporal. On a future all the types will be supported.
      const supportedTypes = ['text', 'password', 'email', 'number', 'url', 'tel', 'search', 'date', 'datetime', 'datetime-local', 'month', 'week', 'time', 'range', 'color']
      if (!supportedTypes.includes(argType)) {
        if (argType == 'number-float') argType = 'number'
        else argType = 'text' // By default
      }

      return argType
    }
  },
  updated () {
    for (const [index, action] of this.actions.entries()) {
      action.order = index
    }
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
#summary-card, .card {
  // background-color: rgb(44, 49, 54),
  background: rgb(36, 40, 44)
}
</style>
